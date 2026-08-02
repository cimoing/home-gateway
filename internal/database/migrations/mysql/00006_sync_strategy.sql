-- +goose Up
ALTER TABLE bt_tasks
    ADD COLUMN sync_strategy VARCHAR(16) NOT NULL DEFAULT 'complete',
    ADD CONSTRAINT chk_bt_tasks_sync_strategy
        CHECK (sync_strategy IN ('complete', 'per_file'));

ALTER TABLE bt_task_files
    ADD COLUMN sync_status VARCHAR(16) NOT NULL DEFAULT 'none',
    ADD COLUMN sync_error VARCHAR(1024) NOT NULL DEFAULT '',
    ADD CONSTRAINT chk_bt_task_files_sync_status
        CHECK (sync_status IN ('none', 'pending', 'syncing', 'synced', 'error'));

-- +goose Down
ALTER TABLE bt_task_files
    DROP CHECK chk_bt_task_files_sync_status,
    DROP COLUMN sync_error,
    DROP COLUMN sync_status;
ALTER TABLE bt_tasks
    DROP CHECK chk_bt_tasks_sync_strategy,
    DROP COLUMN sync_strategy;
