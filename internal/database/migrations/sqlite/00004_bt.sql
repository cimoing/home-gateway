-- +goose Up
CREATE TABLE bt_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash VARCHAR(128) NOT NULL UNIQUE,
    source_type VARCHAR(16) NOT NULL,
    source_value TEXT NOT NULL DEFAULT '',
    metainfo BLOB,
    name VARCHAR(512) NOT NULL DEFAULT '',
    save_path TEXT NOT NULL,
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

CREATE TABLE bt_task_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL,
    file_index INTEGER NOT NULL,
    path TEXT NOT NULL,
    length INTEGER NOT NULL,
    selected INTEGER NOT NULL DEFAULT 1 CHECK (selected IN (0, 1)),
    priority INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT fk_bt_task_files_task
        FOREIGN KEY (task_id) REFERENCES bt_tasks (id) ON DELETE CASCADE,
    UNIQUE (task_id, file_index)
);

CREATE INDEX idx_bt_tasks_status ON bt_tasks (status);
CREATE INDEX idx_bt_task_files_task_id ON bt_task_files (task_id);

-- +goose Down
DROP TABLE IF EXISTS bt_task_files;
DROP TABLE IF EXISTS bt_tasks;
