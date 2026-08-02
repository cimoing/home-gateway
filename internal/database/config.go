package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"home-gateway/internal/datadir"
)

const (
	DriverSQLite = "sqlite"

	defaultSQLiteDSN = "db/home-gateway.db"
)

// Config contains database connection settings.
type Config struct {
	Driver string
	DSN    string
}

// ConfigFromEnv loads database settings and defaults to a local SQLite file.
func ConfigFromEnv() (Config, error) {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("DB_DRIVER")))
	if driver == "" {
		driver = DriverSQLite
	}

	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		dsn = defaultSQLiteDSN
	}
	resolved, err := resolveDSN(dsn)
	if err != nil {
		return Config{}, err
	}

	config := Config{Driver: driver, DSN: resolved}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate verifies that SQLite is selected and configured.
func (c Config) Validate() error {
	if c.Driver != DriverSQLite {
		return fmt.Errorf("unsupported database driver %q; only sqlite is supported", c.Driver)
	}
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("database DSN must not be empty")
	}
	return nil
}

func resolveDSN(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", fmt.Errorf("database DSN must not be empty")
	}
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file::memory:") {
		return dsn, nil
	}
	query := ""
	path := dsn
	if index := strings.IndexByte(dsn, '?'); index >= 0 {
		path = dsn[:index]
		query = dsn[index:]
	}
	path = strings.TrimPrefix(path, "file:")
	if path == "" {
		return dsn, nil
	}
	if filepath.IsAbs(path) {
		return path + query, nil
	}
	resolved, err := datadir.Resolve(path)
	if err != nil {
		return "", fmt.Errorf("resolve database DSN: %w", err)
	}
	return resolved + query, nil
}
