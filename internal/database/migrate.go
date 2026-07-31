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

// Migrate applies all pending migrations for the selected database dialect.
func Migrate(ctx context.Context, db *sqlx.DB, driver string) error {
	dialect, directory, err := migrationSettings(driver)
	if err != nil {
		return err
	}

	// Goose's embedded filesystem and dialect configuration are package-global.
	migrationMu.Lock()
	defer migrationMu.Unlock()

	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db.DB, directory); err != nil {
		return fmt.Errorf("migrate %s database: %w", driver, err)
	}
	return nil
}

func migrationSettings(driver string) (dialect string, directory string, err error) {
	switch driver {
	case DriverSQLite:
		return "sqlite3", "sqlite", nil
	case DriverPostgres:
		return "postgres", "postgres", nil
	case DriverMySQL:
		return "mysql", "mysql", nil
	default:
		return "", "", fmt.Errorf("unsupported database driver %q", driver)
	}
}
