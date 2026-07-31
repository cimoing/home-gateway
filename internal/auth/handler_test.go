package auth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"home-gateway/internal/database"
	userservice "home-gateway/internal/user"

	"github.com/gin-gonic/gin"
)

func TestAuthenticationEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SESSION_SECURE", "false")

	db, err := database.Open(context.Background(), database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "handler.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}
	if err := userservice.NewService(db).Create(
		context.Background(),
		"web-admin",
		[]byte("valid-password"),
	); err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	api := engine.Group("/api")
	NewHandler(NewService(db)).Register(api)

	login := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/auth/login",
		`{"username":"web-admin","password":"valid-password"}`,
		nil,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("expected login status %d, got %d: %s", http.StatusOK, login.Code, login.Body)
	}
	if strings.Contains(login.Body.String(), "password_hash") {
		t.Fatal("login response exposed password hash")
	}

	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}

	session := performJSONRequest(
		engine,
		http.MethodGet,
		"/api/auth/session",
		"",
		cookies[0],
	)
	if session.Code != http.StatusOK {
		t.Fatalf("expected session status %d, got %d: %s", http.StatusOK, session.Code, session.Body)
	}

	logout := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/auth/logout",
		"",
		cookies[0],
	)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("expected logout status %d, got %d", http.StatusNoContent, logout.Code)
	}

	expired := performJSONRequest(
		engine,
		http.MethodGet,
		"/api/auth/session",
		"",
		cookies[0],
	)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked session status %d, got %d", http.StatusUnauthorized, expired.Code)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := database.Open(context.Background(), database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "invalid-login.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}

	engine := gin.New()
	api := engine.Group("/api")
	NewHandler(NewService(db)).Register(api)

	response := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/auth/login",
		`{"username":"missing","password":"wrong-password"}`,
		nil,
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, response.Code, response.Body)
	}
}

func performJSONRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}
