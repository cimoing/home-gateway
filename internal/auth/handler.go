package auth

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

const sessionCookieName = "home_gateway_session"
const userContextKey = "authenticatedUser"

// Handler exposes authentication endpoints.
type Handler struct {
	service      *Service
	secureCookie bool
}

// NewHandler creates an HTTP authentication handler.
func NewHandler(service *Service) *Handler {
	secureCookie, _ := strconv.ParseBool(os.Getenv("SESSION_SECURE"))
	return &Handler{service: service, secureCookie: secureCookie}
}

// Register adds authentication routes to the API group.
func (h *Handler) Register(api *gin.RouterGroup) {
	group := api.Group("/auth")
	group.POST("/login", h.login)
	group.GET("/session", h.session)
	group.POST("/logout", h.logout)
}

// RequireSession rejects requests without an active in-memory session.
func (h *Handler) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookieName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
			c.Abort()
			return
		}
		user, err := h.service.UserForSession(c.Request.Context(), token)
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				h.clearSessionCookie(c)
				c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "session lookup failed"})
			}
			c.Abort()
			return
		}
		c.Set(userContextKey, user)
		c.Next()
	}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) login(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)

	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login request"})
		return
	}

	password := []byte(request.Password)
	defer clear(password)
	token, expiresAt, err := h.service.Login(c.Request.Context(), request.Username, password, LoginMetadata{
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": ErrInvalidCredentials.Error()})
		case errors.Is(err, ErrRateLimited):
			c.Header("Retry-After", "300")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": ErrRateLimited.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		}
		return
	}

	user, err := h.service.UserForSession(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	h.setSessionCookie(c, token, expiresAt)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) session(c *gin.Context) {
	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
		return
	}

	user, err := h.service.UserForSession(c.Request.Context(), token)
	if err != nil {
		h.clearSessionCookie(c)
		if errors.Is(err, ErrUnauthenticated) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session lookup failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) logout(c *gin.Context) {
	token, _ := c.Cookie(sessionCookieName)
	if err := h.service.Logout(c.Request.Context(), token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "logout failed"})
		return
	}
	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *Handler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
