package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"home-gateway/internal/database"
	userservice "home-gateway/internal/user"
)

func TestLoginSessionAndLogout(t *testing.T) {
	testCases := []database.Config{{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "auth.db"),
	}}
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		testCases = append(testCases, database.Config{Driver: database.DriverPostgres, DSN: dsn})
	}
	if dsn := os.Getenv("TEST_MYSQL_DSN"); dsn != "" {
		testCases = append(testCases, database.Config{Driver: database.DriverMySQL, DSN: dsn})
	}

	for _, config := range testCases {
		t.Run(config.Driver, func(t *testing.T) {
			testLoginSessionAndLogout(t, config)
		})
	}
}

func testLoginSessionAndLogout(t *testing.T, config database.Config) {
	t.Helper()
	ctx := context.Background()

	db, err := database.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, config.Driver); err != nil {
		t.Fatal(err)
	}

	const username = "auth_service_admin"
	const password = "valid-password"
	if err := userservice.NewService(db).Create(ctx, username, []byte(password)); err != nil {
		t.Fatal(err)
	}

	service := NewService(db)
	token, _, err := service.Login(ctx, username, []byte(password), LoginMetadata{
		IPAddress: "127.0.0.1",
		UserAgent: strings.Repeat("a", 1025),
	})
	if err != nil {
		t.Fatal(err)
	}
	currentUser, err := service.UserForSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if currentUser.Username != username {
		t.Fatalf("unexpected session user %q", currentUser.Username)
	}

	var userAgentLength int
	query := db.Rebind(`
		SELECT LENGTH(user_agent) FROM user_login_logs
		WHERE username = ? AND success = ? ORDER BY id DESC LIMIT 1
	`)
	if err := db.GetContext(ctx, &userAgentLength, query, username, true); err != nil {
		t.Fatal(err)
	}
	if userAgentLength != 1024 {
		t.Fatalf("expected truncated user agent length 1024, got %d", userAgentLength)
	}

	if err := service.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UserForSession(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated after logout, got %v", err)
	}

	if _, _, err := service.Login(
		ctx,
		"missing_auth_user",
		[]byte("wrong-password"),
		LoginMetadata{IPAddress: "127.0.0.2"},
	); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}
