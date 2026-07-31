package command

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"home-gateway/internal/database"

	"golang.org/x/crypto/bcrypt"
)

func TestUserCreateAndPasswordCommands(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "commands.db")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_DSN", dsn)

	executeCommand(
		t,
		[]string{"user", "create", "admin", "--password-stdin"},
		"\xEF\xBB\xBFinitial-password\r\n",
	)
	assertCommandPassword(t, dsn, "admin", "initial-password")

	executeCommand(t, []string{"user", "passwd", "admin", "--password-stdin"}, "updated-password\n")
	assertCommandPassword(t, dsn, "admin", "updated-password")
}

func executeCommand(t *testing.T, args []string, input string) {
	t.Helper()

	command := newRootCommand()
	command.SetArgs(args)
	command.SetIn(bytes.NewBufferString(input))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func assertCommandPassword(t *testing.T, dsn string, username string, password string) {
	t.Helper()

	db, err := database.Open(context.Background(), database.Config{
		Driver: database.DriverSQLite,
		DSN:    dsn,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var hash string
	if err := db.Get(&hash, `SELECT password_hash FROM users WHERE username = ?`, username); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatalf("password does not match stored hash: %v", err)
	}
}
