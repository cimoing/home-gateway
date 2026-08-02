package database

import (
	"fmt"
	"os"
	"strings"
)

const (
	DriverSQLite = "sqlite"

	defaultSQLiteDSN = "/data/home-gateway.db"
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

	config := Config{Driver: driver, DSN: dsn}
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
