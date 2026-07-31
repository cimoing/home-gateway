package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"home-gateway/internal/model"

	"github.com/jmoiron/sqlx"
)

func TestConfigFromEnvDefaultsToSQLite(t *testing.T) {
	t.Setenv("DB_DRIVER", "")
	t.Setenv("DB_DSN", "")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Driver != DriverSQLite {
		t.Fatalf("expected driver %q, got %q", DriverSQLite, config.Driver)
	}
	if config.DSN != defaultSQLiteDSN {
		t.Fatalf("expected DSN %q, got %q", defaultSQLiteDSN, config.DSN)
	}
}

func TestSupportedDatabases(t *testing.T) {
	testCases := []struct {
		name   string
		driver string
		dsn    string
	}{
		{
			name:   "sqlite",
			driver: DriverSQLite,
			dsn:    filepath.Join(t.TempDir(), "gateway.db"),
		},
	}
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		testCases = append(testCases, struct {
			name   string
			driver string
			dsn    string
		}{name: "postgres", driver: DriverPostgres, dsn: dsn})
	}
	if dsn := os.Getenv("TEST_MYSQL_DSN"); dsn != "" {
		testCases = append(testCases, struct {
			name   string
			driver string
			dsn    string
		}{name: "mysql", driver: DriverMySQL, dsn: dsn})
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testDatabase(t, Config{Driver: testCase.driver, DSN: testCase.dsn})
		})
	}
}

func testDatabase(t *testing.T, config Config) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(ctx, db, config.Driver); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, config.Driver); err != nil {
		t.Fatalf("migrations must be repeatable: %v", err)
	}

	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("integration_%d", suffix)
	email := fmt.Sprintf("integration_%d@example.test", suffix)

	insertUser := db.Rebind(`
		INSERT INTO users (username, password_hash, display_name, email, enabled)
		VALUES (?, ?, ?, ?, ?)
	`)
	if _, err := db.ExecContext(ctx, insertUser, username, "hash", "Integration Test", email, true); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var user model.User
	selectUser := db.Rebind(`
		SELECT id, username, password_hash, display_name, email, enabled,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE username = ?
	`)
	if err := db.GetContext(ctx, &user, selectUser, username); err != nil {
		t.Fatalf("select user: %v", err)
	}
	if user.Email == nil || *user.Email != email || !user.Enabled {
		t.Fatalf("unexpected user: %+v", user)
	}

	if _, err := db.ExecContext(ctx, insertUser, username, "other-hash", "", nil, true); err == nil {
		t.Fatal("expected duplicate username to be rejected")
	}

	insertLog := db.Rebind(`
		INSERT INTO user_login_logs
		    (user_id, username, success, failure_reason, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	result, err := db.ExecContext(ctx, insertLog, user.ID, username, true, nil, "127.0.0.1", "integration-test")
	if err != nil {
		t.Fatalf("insert login log: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		insertLog,
		user.ID,
		username,
		true,
		nil,
		"127.0.0.1",
		strings.Repeat("a", 1025),
	); err == nil {
		t.Fatal("expected user agent longer than 1024 characters to be rejected")
	}

	logID, err := result.LastInsertId()
	if err != nil && config.Driver == DriverPostgres {
		selectLogID := db.Rebind(`
			SELECT id FROM user_login_logs
			WHERE username = ? ORDER BY id DESC LIMIT 1
		`)
		if err := db.GetContext(ctx, &logID, selectLogID, username); err != nil {
			t.Fatalf("select login log id: %v", err)
		}
	} else if err != nil {
		t.Fatalf("get login log id: %v", err)
	}

	deleteUser := db.Rebind(`DELETE FROM users WHERE id = ?`)
	if _, err := db.ExecContext(ctx, deleteUser, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var userID sql.NullInt64
	selectLogUser := db.Rebind(`SELECT user_id FROM user_login_logs WHERE id = ?`)
	if err := db.GetContext(ctx, &userID, selectLogUser, logID); err != nil {
		t.Fatalf("select login log: %v", err)
	}
	if userID.Valid {
		t.Fatal("expected login log user_id to be null after deleting user")
	}

	testDNSSchema(t, ctx, db, suffix)
}

func testDNSSchema(t *testing.T, ctx context.Context, db *sqlx.DB, suffix int64) {
	t.Helper()
	name := fmt.Sprintf("credential_%d", suffix)
	fingerprint := fmt.Sprintf("%064x", suffix)
	insertCredential := db.Rebind(`
		INSERT INTO cloudflare_credentials
		    (name, token_ciphertext, token_nonce, token_fingerprint, token_hint)
		VALUES (?, ?, ?, ?, ?)
	`)
	result, err := db.ExecContext(
		ctx,
		insertCredential,
		name,
		[]byte("encrypted-token"),
		[]byte("random-nonce"),
		fingerprint,
		"oken",
	)
	if err != nil {
		t.Fatalf("insert Cloudflare credential: %v", err)
	}
	credentialID, err := result.LastInsertId()
	if err != nil || credentialID == 0 {
		query := db.Rebind(`SELECT id FROM cloudflare_credentials WHERE name = ?`)
		if err := sqlx.GetContext(ctx, db, &credentialID, query, name); err != nil {
			t.Fatalf("select Cloudflare credential: %v", err)
		}
	}
	var ciphertext []byte
	queryCiphertext := db.Rebind(`SELECT token_ciphertext FROM cloudflare_credentials WHERE id = ?`)
	if err := sqlx.GetContext(ctx, db, &ciphertext, queryCiphertext, credentialID); err != nil {
		t.Fatalf("select encrypted token: %v", err)
	}
	if string(ciphertext) != "encrypted-token" {
		t.Fatalf("unexpected encrypted token bytes %q", ciphertext)
	}

	zoneProviderID := fmt.Sprintf("zone-%d", suffix)
	zoneName := fmt.Sprintf("zone-%d.example.test", suffix)
	insertZone := db.Rebind(`
		INSERT INTO dns_zones (credential_id, provider_zone_id, name, status)
		VALUES (?, ?, ?, ?)
	`)
	result, err = db.ExecContext(ctx, insertZone, credentialID, zoneProviderID, zoneName, "active")
	if err != nil {
		t.Fatalf("insert DNS zone: %v", err)
	}
	zoneID, err := result.LastInsertId()
	if err != nil || zoneID == 0 {
		query := db.Rebind(`SELECT id FROM dns_zones WHERE provider_zone_id = ?`)
		if err := sqlx.GetContext(ctx, db, &zoneID, query, zoneProviderID); err != nil {
			t.Fatalf("select DNS zone: %v", err)
		}
	}

	insertRecord := db.Rebind(`
		INSERT INTO dns_records
		    (zone_id, provider_record_id, type, name, content, ttl, data_json, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := db.ExecContext(
		ctx,
		insertRecord,
		zoneID,
		fmt.Sprintf("record-%d", suffix),
		"A",
		zoneName,
		"192.0.2.1",
		1,
		"{}",
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("insert DNS record: %v", err)
	}
	deleteZone := db.Rebind(`DELETE FROM dns_zones WHERE id = ?`)
	if _, err := db.ExecContext(ctx, deleteZone, zoneID); err != nil {
		t.Fatalf("delete DNS zone: %v", err)
	}
	var records int
	countRecords := db.Rebind(`SELECT COUNT(*) FROM dns_records WHERE zone_id = ?`)
	if err := sqlx.GetContext(ctx, db, &records, countRecords, zoneID); err != nil {
		t.Fatalf("count DNS records: %v", err)
	}
	if records != 0 {
		t.Fatal("expected DNS records to cascade delete with zone")
	}
}
