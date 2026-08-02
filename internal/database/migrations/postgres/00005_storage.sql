-- +goose Up
CREATE TABLE storage_backends (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    type VARCHAR(16) NOT NULL CHECK (type IN ('local', 'smb', 's3')),
    config_json TEXT NOT NULL DEFAULT '{}',
    secret_ciphertext BYTEA,
    secret_nonce BYTEA,
    secret_fingerprint CHAR(64),
    secret_hint VARCHAR(4) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_storage_backends_secret_fingerprint
    ON storage_backends (secret_fingerprint)
    WHERE secret_fingerprint IS NOT NULL;

ALTER TABLE bt_tasks
    ADD COLUMN storage_backend_id BIGINT REFERENCES storage_backends (id) ON DELETE SET NULL,
    ADD COLUMN storage_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none'
        CHECK (sync_status IN ('none', 'pending', 'syncing', 'synced', 'error')),
    ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bt_tasks
    DROP COLUMN IF EXISTS sync_error,
    DROP COLUMN IF EXISTS sync_status,
    DROP COLUMN IF EXISTS storage_prefix,
    DROP COLUMN IF EXISTS storage_backend_id;
DROP TABLE IF EXISTS storage_backends;
