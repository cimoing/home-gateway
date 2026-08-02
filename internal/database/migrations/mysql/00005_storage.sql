-- +goose Up
CREATE TABLE storage_backends (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    type VARCHAR(16) NOT NULL,
    config_json TEXT NOT NULL,
    secret_ciphertext BLOB,
    secret_nonce VARBINARY(64),
    secret_fingerprint CHAR(64),
    secret_hint VARCHAR(4) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_storage_backends_name (name),
    UNIQUE KEY uq_storage_backends_secret_fingerprint (secret_fingerprint),
    CHECK (type IN ('local', 'smb', 's3'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE bt_tasks
    ADD COLUMN storage_backend_id BIGINT UNSIGNED NULL,
    ADD COLUMN storage_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '',
    ADD CONSTRAINT fk_bt_tasks_storage_backend
        FOREIGN KEY (storage_backend_id) REFERENCES storage_backends (id) ON DELETE SET NULL,
    ADD CONSTRAINT chk_bt_tasks_sync_status
        CHECK (sync_status IN ('none', 'pending', 'syncing', 'synced', 'error'));

-- +goose Down
ALTER TABLE bt_tasks
    DROP FOREIGN KEY fk_bt_tasks_storage_backend,
    DROP CHECK chk_bt_tasks_sync_status,
    DROP COLUMN sync_error,
    DROP COLUMN sync_status,
    DROP COLUMN storage_prefix,
    DROP COLUMN storage_backend_id;
DROP TABLE IF EXISTS storage_backends;
