-- +goose Up
ALTER TABLE bt_task_files ADD COLUMN synced_bytes BIGINT NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite cannot drop columns portably in older versions; leave columns in place.
SELECT 1;
