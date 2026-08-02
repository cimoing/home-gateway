package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Open establishes and verifies a SQLite database connection.
func Open(ctx context.Context, config Config) (*sqlx.DB, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	if err := ensureSQLiteDirectory(config.DSN); err != nil {
		return nil, err
	}

	dsn, err := sqliteDSN(config.DSN)
	if err != nil {
		return nil, err
	}

	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	return db, nil
}

func sqliteDSN(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("database DSN must not be empty")
	}
	separator := "?"
	if strings.Contains(raw, "?") {
		separator = "&"
	}
	return raw + separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", nil
}

func ensureSQLiteDirectory(dsn string) error {
	path := strings.TrimSpace(dsn)
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil
	}
	if query := strings.IndexByte(path, '?'); query >= 0 {
		path = path[:query]
	}
	path = strings.TrimPrefix(path, "file:")
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}
