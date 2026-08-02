package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"home-gateway/internal/bt"
	appconfig "home-gateway/internal/config"
	"home-gateway/internal/database"

	"github.com/gin-gonic/gin"
)

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	New().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	if body := recorder.Body.String(); body != `{"status":"ok"}` {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestServesWebAppAndSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	webRoot := t.TempDir()
	index := []byte("<html>home gateway</html>")
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), index, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/devices/overview"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		NewWithWebRoot(webRoot).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected status %d, got %d", path, http.StatusOK, recorder.Code)
		}
		if body := recorder.Body.String(); body != string(index) {
			t.Fatalf("%s: unexpected response body: %s", path, body)
		}
	}
}

func TestUnknownAPIRouteDoesNotServeWebApp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	NewWithWebRoot(t.TempDir()).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestDNSRoutesRequireSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.Open(context.Background(), database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "router.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/dns/zones", nil)
	New(db).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}

func TestBTRoutesRequireSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.Open(context.Background(), database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "bt-router.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}
	service := bt.NewService(db, nil, appconfig.Default().BT, "")
	defer service.Close()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/bt/settings", nil)
	NewWithServices(db, service, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
}
