-- +goose Up
ALTER TABLE bt_tasks
    ADD COLUMN sync_strategy VARCHAR(16) NOT NULL DEFAULT 'complete'
        CHECK (sync_strategy IN ('complete', 'per_file'));

ALTER TABLE bt_task_files
    ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none'
        CHECK (sync_status IN ('none', 'pending', 'syncing', 'synced', 'error')),
    ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE bt_task_files
    DROP COLUMN IF EXISTS sync_error,
    DROP COLUMN IF EXISTS sync_status;
ALTER TABLE bt_tasks
    DROP COLUMN IF EXISTS sync_strategy;
