package database

import (
	"fmt"
	"os"
	"strings"
)

const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"

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
	if driver == "pgsql" || driver == "postgresql" {
		driver = DriverPostgres
	}

	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		if driver != DriverSQLite {
			return Config{}, fmt.Errorf("DB_DSN is required for %s", driver)
		}
		dsn = defaultSQLiteDSN
	}

	config := Config{Driver: driver, DSN: dsn}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate verifies that the selected database is supported and configured.
func (c Config) Validate() error {
	switch c.Driver {
	case DriverSQLite, DriverPostgres, DriverMySQL:
	default:
		return fmt.Errorf("unsupported database driver %q", c.Driver)
	}
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("database DSN must not be empty")
	}
	return nil
}
