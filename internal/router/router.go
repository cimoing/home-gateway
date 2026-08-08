package router

import (
	"net/http"
	"os"
	"path"
	"strings"

	"home-gateway/internal/auth"
	"home-gateway/internal/bt"
	"home-gateway/internal/dns"
	"home-gateway/internal/hostmetrics"
	"home-gateway/internal/storage"
	"home-gateway/internal/webui"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// Features lists optional modules available to the signed-in UI.
type Features struct {
	BT bool `json:"bt"`
}

// Services bundles runtime dependencies for the HTTP router.
type Services struct {
	Database *sqlx.DB
	BT       *bt.Service
	Storage  *storage.Service
	DNS      *dns.Service
	Features func() Features
	Reload   func() error
}

// New creates the application's HTTP router.
func New(databases ...*sqlx.DB) *gin.Engine {
	return newRouter(resolveWebFS(), Services{Database: firstDatabase(databases)})
}

// NewWithWebRoot creates the router and serves a web app from disk.
func NewWithWebRoot(webRoot string, databases ...*sqlx.DB) *gin.Engine {
	return newRouter(http.Dir(webRoot), Services{Database: firstDatabase(databases)})
}

// NewWithServices creates the production router with runtime services.
func NewWithServices(services Services) *gin.Engine {
	return newRouter(resolveWebFS(), services)
}

func resolveWebFS() http.FileSystem {
	if root := strings.TrimSpace(os.Getenv("WEB_ROOT")); root != "" {
		return http.Dir(root)
	}
	return http.FS(webui.FS())
}

func newRouter(webFS http.FileSystem, services Services) *gin.Engine {
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
		protected.GET("/system/features", func(c *gin.Context) {
			features := Features{}
			if services.Features != nil {
				features = services.Features()
			} else if services.BT != nil {
				features.BT = true
			}
			c.JSON(http.StatusOK, gin.H{"features": features})
		})
		protected.GET("/system/metrics", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"metrics": hostmetrics.Collect()})
		})
		if services.DNS != nil {
			dns.NewHandler(services.DNS).Register(protected)
		}
		if services.Storage != nil {
			storage.NewHandler(services.Storage).Register(protected)
		}
		if services.BT != nil {
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

	if webFS != nil {
		engine.NoRoute(spaHandler(webFS))
	}

	return engine
}

func spaHandler(webFS http.FileSystem) gin.HandlerFunc {
	fileServer := http.FileServer(webFS)
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		requestPath := path.Clean("/" + c.Request.URL.Path)
		if requestPath == "/" {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		relative := strings.TrimPrefix(requestPath, "/")
		if !webFileExists(webFS, relative) {
			c.Request.URL.Path = "/"
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

func webFileExists(webFS http.FileSystem, name string) bool {
	file, err := webFS.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func firstDatabase(databases []*sqlx.DB) *sqlx.DB {
	if len(databases) == 0 {
		return nil
	}
	return databases[0]
}
