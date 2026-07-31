package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"home-gateway/internal/auth"
	"home-gateway/internal/bt"
	"home-gateway/internal/credential"
	"home-gateway/internal/dns"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// New creates the application's HTTP router.
func New(databases ...*sqlx.DB) *gin.Engine {
	return newRouter(os.Getenv("WEB_ROOT"), firstDatabase(databases), nil)
}

// NewWithWebRoot creates the router and optionally serves a built web app.
func NewWithWebRoot(webRoot string, databases ...*sqlx.DB) *gin.Engine {
	return newRouter(webRoot, firstDatabase(databases), nil)
}

// NewWithServices creates the production router with runtime services.
func NewWithServices(database *sqlx.DB, btService *bt.Service) *gin.Engine {
	return newRouter(os.Getenv("WEB_ROOT"), database, btService)
}

func newRouter(webRoot string, database *sqlx.DB, btService *bt.Service) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())
	_ = engine.SetTrustedProxies(nil)

	api := engine.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
	if database != nil {
		authHandler := auth.NewHandler(auth.NewService(database))
		authHandler.Register(api)
		protected := api.Group("")
		protected.Use(authHandler.RequireSession())
		dns.NewHandler(
			dns.NewService(database, credential.FromEnv()),
		).Register(protected)
		if btService != nil {
			bt.NewHandler(btService).Register(protected)
		}
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

func firstDatabase(databases []*sqlx.DB) *sqlx.DB {
	if len(databases) == 0 {
		return nil
	}
	return databases[0]
}
