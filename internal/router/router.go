package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"home-gateway/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// New creates the application's HTTP router.
func New(databases ...*sqlx.DB) *gin.Engine {
	return NewWithWebRoot(os.Getenv("WEB_ROOT"), databases...)
}

// NewWithWebRoot creates the router and optionally serves a built web app.
func NewWithWebRoot(webRoot string, databases ...*sqlx.DB) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())
	_ = engine.SetTrustedProxies(nil)

	api := engine.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
	if len(databases) > 0 && databases[0] != nil {
		auth.NewHandler(auth.NewService(databases[0])).Register(api)
	}

	if webRoot != "" {
		fileServer := http.FileServer(http.Dir(webRoot))
		engine.NoRoute(func(c *gin.Context) {
			if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}

			relativePath := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(c.Request.URL.Path, "/")))
			requestedPath := filepath.Join(webRoot, relativePath)
			if strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
				c.Request.URL.Path = "/"
			} else if info, err := os.Stat(requestedPath); err != nil || info.IsDir() {
				c.Request.URL.Path = "/"
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	return engine
}
