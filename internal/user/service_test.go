package user

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"home-gateway/internal/database"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

func TestCreateAndUpdatePassword(t *testing.T) {
	ctx := context.Background()
	config := database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "users.db"),
	}

	db, err := database.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db, config.Driver); err != nil {
		t.Fatal(err)
	}

	service := NewService(db)
	if err := service.Create(ctx, "admin", []byte("initial-password")); err != nil {
		t.Fatal(err)
	}
	if err := service.Create(ctx, "admin", []byte("other-password")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	assertPassword(t, db, "admin", "initial-password")

	if err := service.UpdatePassword(ctx, "admin", []byte("updated-password")); err != nil {
		t.Fatal(err)
	}
	assertPassword(t, db, "admin", "updated-password")

	if err := service.UpdatePassword(ctx, "missing", []byte("updated-password")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCredentialValidation(t *testing.T) {
	if err := ValidateUsername("contains space"); err == nil {
		t.Fatal("expected username with whitespace to be rejected")
	}
	if err := ValidatePassword([]byte("short")); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	if err := ValidatePassword(make([]byte, 73)); err == nil {
		t.Fatal("expected password over bcrypt limit to be rejected")
	}
}

func assertPassword(t *testing.T, db *sqlx.DB, username string, password string) {
	t.Helper()

	var hash string
	query := db.Rebind(`SELECT password_hash FROM users WHERE username = ?`)
	if err := db.Get(&hash, query, username); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatalf("password does not match stored hash: %v", err)
	}
}
