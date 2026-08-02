package database

import (
	"context"
	"database/sql"
	"fmt"
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

func TestConfigFromEnvRejectsNonSQLite(t *testing.T) {
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DB_DSN", "postgres://example")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected non-sqlite driver to be rejected")
	}
}

func TestSQLiteSchema(t *testing.T) {
	testDatabase(t, Config{
		Driver: DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "gateway.db"),
	})
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
	if err != nil {
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

	testBTSchema(t, ctx, db, suffix)
}

func testBTSchema(t *testing.T, ctx context.Context, db *sqlx.DB, suffix int64) {
	t.Helper()
	hash := fmt.Sprintf("%040x", suffix)
	insertTask := db.Rebind(`
		INSERT INTO bt_tasks
		    (info_hash, source_type, source_value, name, save_path,
		     desired_state, status, total_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	result, err := db.ExecContext(
		ctx, insertTask, hash, "magnet", "magnet:?xt=urn:btih:"+hash,
		"integration", "/data/downloads", "paused", "paused", 12,
	)
	if err != nil {
		t.Fatalf("insert BT task: %v", err)
	}
	taskID, err := result.LastInsertId()
	if err != nil || taskID == 0 {
		query := db.Rebind(`SELECT id FROM bt_tasks WHERE info_hash = ?`)
		if err := db.GetContext(ctx, &taskID, query, hash); err != nil {
			t.Fatalf("select BT task: %v", err)
		}
	}
	insertFile := db.Rebind(`
		INSERT INTO bt_task_files
		    (task_id, file_index, path, length, selected, priority)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if _, err := db.ExecContext(
		ctx, insertFile, taskID, 0, "bundle/file.bin", 12, true, 1,
	); err != nil {
		t.Fatalf("insert BT task file: %v", err)
	}
	if _, err := db.ExecContext(ctx, insertFile, taskID, 0, "duplicate", 1, true, 1); err == nil {
		t.Fatal("expected duplicate BT file index to be rejected")
	}
	deleteTask := db.Rebind(`DELETE FROM bt_tasks WHERE id = ?`)
	if _, err := db.ExecContext(ctx, deleteTask, taskID); err != nil {
		t.Fatalf("delete BT task: %v", err)
	}
	var files int
	countFiles := db.Rebind(`SELECT COUNT(*) FROM bt_task_files WHERE task_id = ?`)
	if err := db.GetContext(ctx, &files, countFiles, taskID); err != nil {
		t.Fatalf("count BT files: %v", err)
	}
	if files != 0 {
		t.Fatal("expected BT files to cascade delete with task")
	}
}
