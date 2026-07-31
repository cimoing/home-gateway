package model

import "time"

// UserSession represents a revocable browser session.
type UserSession struct {
	TokenHash  string    `db:"token_hash" json:"-"`
	UserID     int64     `db:"user_id" json:"userId"`
	ExpiresAt  time.Time `db:"expires_at" json:"expiresAt"`
	CreatedAt  time.Time `db:"created_at" json:"createdAt"`
	LastSeenAt time.Time `db:"last_seen_at" json:"lastSeenAt"`
}
