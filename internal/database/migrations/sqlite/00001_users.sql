-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    email TEXT UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at DATETIME
);

CREATE TABLE user_login_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    username TEXT NOT NULL,
    success INTEGER NOT NULL CHECK (success IN (0, 1)),
    failure_reason TEXT,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent VARCHAR(1024) NOT NULL DEFAULT ''
        CHECK (length(user_agent) <= 1024),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_login_logs_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
);

CREATE INDEX idx_user_login_logs_user_id ON user_login_logs (user_id);
CREATE INDEX idx_user_login_logs_created_at ON user_login_logs (created_at);

-- +goose Down
DROP TABLE IF EXISTS user_login_logs;
DROP TABLE IF EXISTS users;
