package model

import "time"

// User represents an account that can authenticate with the gateway.
type User struct {
	ID           int64      `db:"id" json:"id"`
	Username     string     `db:"username" json:"username"`
	PasswordHash string     `db:"password_hash" json:"-"`
	DisplayName  string     `db:"display_name" json:"displayName"`
	Email        *string    `db:"email" json:"email,omitempty"`
	Enabled      bool       `db:"enabled" json:"enabled"`
	CreatedAt    time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updatedAt"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"lastLoginAt,omitempty"`
}

// UserLoginLog records both successful and failed authentication attempts.
type UserLoginLog struct {
	ID            int64     `db:"id" json:"id"`
	UserID        *int64    `db:"user_id" json:"userId,omitempty"`
	Username      string    `db:"username" json:"username"`
	Success       bool      `db:"success" json:"success"`
	FailureReason *string   `db:"failure_reason" json:"failureReason,omitempty"`
	IPAddress     string    `db:"ip_address" json:"ipAddress"`
	UserAgent     string    `db:"user_agent" json:"userAgent"`
	CreatedAt     time.Time `db:"created_at" json:"createdAt"`
}
