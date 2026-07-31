-- +goose Up
CREATE TABLE bt_tasks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    info_hash VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_type VARCHAR(16) NOT NULL,
    source_value TEXT NOT NULL,
    metainfo LONGBLOB,
    name VARCHAR(512) NOT NULL DEFAULT '',
    save_path TEXT NOT NULL,
    desired_state VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL,
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    total_bytes BIGINT NOT NULL DEFAULT 0,
    completed_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_bt_tasks_info_hash (info_hash),
    KEY idx_bt_tasks_status (status),
    CHECK (source_type IN ('magnet', 'torrent')),
    CHECK (desired_state IN ('downloading', 'paused')),
    CHECK (status IN ('metadata', 'downloading', 'paused', 'completed', 'error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE bt_task_files (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    task_id BIGINT UNSIGNED NOT NULL,
    file_index INT NOT NULL,
    path TEXT NOT NULL,
    length BIGINT NOT NULL,
    selected BOOLEAN NOT NULL DEFAULT TRUE,
    priority INT NOT NULL DEFAULT 1,
    PRIMARY KEY (id),
    UNIQUE KEY uq_bt_task_files_task_index (task_id, file_index),
    KEY idx_bt_task_files_task_id (task_id),
    CONSTRAINT fk_bt_task_files_task
        FOREIGN KEY (task_id) REFERENCES bt_tasks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS bt_task_files;
DROP TABLE IF EXISTS bt_tasks;
