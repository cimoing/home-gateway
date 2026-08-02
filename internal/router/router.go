package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"home-gateway/internal/auth"
	"home-gateway/internal/bt"
	"home-gateway/internal/dns"
	"home-gateway/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// Services bundles runtime dependencies for the HTTP router.
type Services struct {
	Database *sqlx.DB
	BT       *bt.Service
	Storage  *storage.Service
	DNS      *dns.Service
	Reload   func() error
}

// New creates the application's HTTP router.
func New(databases ...*sqlx.DB) *gin.Engine {
	return newRouter(os.Getenv("WEB_ROOT"), Services{Database: firstDatabase(databases)})
}

// NewWithWebRoot creates the router and optionally serves a built web app.
func NewWithWebRoot(webRoot string, databases ...*sqlx.DB) *gin.Engine {
	return newRouter(webRoot, Services{Database: firstDatabase(databases)})
}

// NewWithServices creates the production router with runtime services.
func NewWithServices(services Services) *gin.Engine {
	return newRouter(os.Getenv("WEB_ROOT"), services)
}

func newRouter(webRoot string, services Services) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())
	_ = engine.SetTrustedProxies(nil)

	api := engine.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
	if services.Database != nil {
		authHandler := auth.NewHandler(auth.NewService(services.Database))
		authHandler.Register(api)
		protected := api.Group("")
		protected.Use(authHandler.RequireSession())
		if services.DNS != nil {
			dns.NewHandler(services.DNS).Register(protected)
		}
		if services.Storage != nil {
			storage.NewHandler(services.Storage).Register(protected)
		}
		if services.BT != nil {
			if services.Storage != nil {
				services.BT.SetStorage(services.Storage)
			}
			bt.NewHandler(services.BT).Register(protected)
		}
		if services.Reload != nil {
			protected.POST("/system/reload-config", func(c *gin.Context) {
				if err := services.Reload(); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
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
