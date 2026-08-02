package database

import (
	"context"
	"fmt"
	"sync"

	"home-gateway/internal/database/migrations"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

var migrationMu sync.Mutex

// Migrate applies all pending SQLite migrations.
func Migrate(ctx context.Context, db *sqlx.DB, driver string) error {
	if driver != DriverSQLite {
		return fmt.Errorf("unsupported database driver %q; only sqlite is supported", driver)
	}

	// Goose's embedded filesystem and dialect configuration are package-global.
	migrationMu.Lock()
	defer migrationMu.Unlock()

	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db.DB, "sqlite"); err != nil {
		return fmt.Errorf("migrate sqlite database: %w", err)
	}
	return nil
}
