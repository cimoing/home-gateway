-- +goose Up
CREATE TABLE bt_tasks (
    id BIGSERIAL PRIMARY KEY,
    info_hash VARCHAR(128) NOT NULL UNIQUE,
    source_type VARCHAR(16) NOT NULL CHECK (source_type IN ('magnet', 'torrent')),
    source_value TEXT NOT NULL DEFAULT '',
    metainfo BYTEA,
    name VARCHAR(512) NOT NULL DEFAULT '',
    save_path TEXT NOT NULL,
    desired_state VARCHAR(16) NOT NULL CHECK (desired_state IN ('downloading', 'paused')),
    status VARCHAR(16) NOT NULL CHECK (status IN ('metadata', 'downloading', 'paused', 'completed', 'error')),
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    total_bytes BIGINT NOT NULL DEFAULT 0,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE bt_task_files (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    file_index INTEGER NOT NULL,
    path TEXT NOT NULL,
    length BIGINT NOT NULL,
    selected BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT fk_bt_task_files_task
        FOREIGN KEY (task_id) REFERENCES bt_tasks (id) ON DELETE CASCADE,
    CONSTRAINT uq_bt_task_files_task_index UNIQUE (task_id, file_index)
);

CREATE INDEX idx_bt_tasks_status ON bt_tasks (status);
CREATE INDEX idx_bt_task_files_task_id ON bt_task_files (task_id);

-- +goose Down
DROP TABLE IF EXISTS bt_task_files;
DROP TABLE IF EXISTS bt_tasks;
