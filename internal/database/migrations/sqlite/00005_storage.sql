-- +goose Up
CREATE TABLE storage_backends (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(128) NOT NULL UNIQUE,
    type VARCHAR(16) NOT NULL,
    config_json TEXT NOT NULL DEFAULT '{}',
    secret_ciphertext BLOB,
    secret_nonce BLOB,
    secret_fingerprint CHAR(64),
    secret_hint VARCHAR(4) NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (type IN ('local', 'smb', 's3'))
);

CREATE UNIQUE INDEX idx_storage_backends_secret_fingerprint
    ON storage_backends (secret_fingerprint)
    WHERE secret_fingerprint IS NOT NULL;

ALTER TABLE bt_tasks ADD COLUMN storage_backend_id INTEGER REFERENCES storage_backends (id) ON DELETE SET NULL;
ALTER TABLE bt_tasks ADD COLUMN storage_prefix TEXT NOT NULL DEFAULT '';
ALTER TABLE bt_tasks ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE bt_tasks ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
-- SQLite cannot cheaply drop columns; drop dependent sync fields by leave-and-recreate is avoided.
-- Down path removes storage table after clearing FK references.
UPDATE bt_tasks SET storage_backend_id = NULL;
DROP TABLE IF EXISTS storage_backends;
