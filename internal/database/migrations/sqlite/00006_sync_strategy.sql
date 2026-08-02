-- +goose Up
ALTER TABLE bt_tasks ADD COLUMN sync_strategy VARCHAR(16) NOT NULL DEFAULT 'complete';
ALTER TABLE bt_task_files ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE bt_task_files ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
-- SQLite cannot drop columns portably in older versions; leave columns in place.
SELECT 1;
