package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Open establishes and verifies a database connection.
func Open(ctx context.Context, config Config) (*sqlx.DB, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	driverName, dsn, err := connectionSettings(config)
	if err != nil {
		return nil, err
	}

	db, err := sqlx.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", config.Driver, err)
	}

	configurePool(db, config.Driver)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s database: %w", config.Driver, err)
	}
	return db, nil
}

func connectionSettings(config Config) (string, string, error) {
	switch config.Driver {
	case DriverSQLite:
		separator := "?"
		if strings.Contains(config.DSN, "?") {
			separator = "&"
		}
		dsn := config.DSN + separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		return "sqlite", dsn, nil
	case DriverPostgres:
		return "pgx", config.DSN, nil
	case DriverMySQL:
		mysqlConfig, err := mysql.ParseDSN(config.DSN)
		if err != nil {
			return "", "", fmt.Errorf("parse mysql DSN: %w", err)
		}
		mysqlConfig.ParseTime = true
		mysqlConfig.Loc = time.UTC
		return "mysql", mysqlConfig.FormatDSN(), nil
	default:
		return "", "", fmt.Errorf("unsupported database driver %q", config.Driver)
	}
}

func configurePool(db *sqlx.DB, driver string) {
	if driver == DriverSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
}
