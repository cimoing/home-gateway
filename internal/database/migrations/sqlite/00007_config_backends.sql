-- +goose Up
PRAGMA foreign_keys = OFF;

DROP TABLE IF EXISTS dns_records;
DROP TABLE IF EXISTS dns_zones;
DROP TABLE IF EXISTS cloudflare_credentials;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS storage_backends;

CREATE TABLE bt_tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash VARCHAR(128) NOT NULL UNIQUE,
    source_type VARCHAR(16) NOT NULL,
    source_value TEXT NOT NULL DEFAULT '',
    metainfo BLOB,
    name VARCHAR(512) NOT NULL DEFAULT '',
    save_path TEXT NOT NULL,
    storage_backend_name TEXT NOT NULL DEFAULT '',
    storage_prefix TEXT NOT NULL DEFAULT '',
    sync_strategy VARCHAR(16) NOT NULL DEFAULT 'complete',
    sync_status VARCHAR(16) NOT NULL DEFAULT 'none',
    sync_error VARCHAR(1024) NOT NULL DEFAULT '',
    desired_state VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL,
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    total_bytes INTEGER NOT NULL DEFAULT 0,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (source_type IN ('magnet', 'torrent')),
    CHECK (desired_state IN ('downloading', 'paused')),
    CHECK (status IN ('metadata', 'downloading', 'paused', 'completed', 'error'))
);

INSERT INTO bt_tasks_new (
    id, info_hash, source_type, source_value, metainfo, name, save_path,
    storage_backend_name, storage_prefix, sync_strategy, sync_status, sync_error,
    desired_state, status, error_message, total_bytes, completed_at, created_at, updated_at
)
SELECT
    id, info_hash, source_type, source_value, metainfo, name, save_path,
    '', storage_prefix, sync_strategy, sync_status, sync_error,
    desired_state, status, error_message, total_bytes, completed_at, created_at, updated_at
FROM bt_tasks;

DROP TABLE bt_tasks;
ALTER TABLE bt_tasks_new RENAME TO bt_tasks;
CREATE INDEX idx_bt_tasks_status ON bt_tasks (status);

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
