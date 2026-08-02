package model

import "time"

const (
	StorageTypeLocal = "local"
	StorageTypeSMB   = "smb"
	StorageTypeS3    = "s3"

	BTSyncNone    = "none"
	BTSyncPending = "pending"
	BTSyncSyncing = "syncing"
	BTSyncSynced  = "synced"
	BTSyncError   = "error"
)

// StorageBackend is a configured storage destination.
type StorageBackend struct {
	ID               int64      `db:"id" json:"id"`
	Name             string     `db:"name" json:"name"`
	Type             string     `db:"type" json:"type"`
	ConfigJSON       string     `db:"config_json" json:"-"`
	SecretCiphertext []byte     `db:"secret_ciphertext" json:"-"`
	SecretNonce      []byte     `db:"secret_nonce" json:"-"`
	SecretFingerprint *string   `db:"secret_fingerprint" json:"-"`
	SecretHint       string     `db:"secret_hint" json:"secretHint,omitempty"`
	HasSecret        bool       `db:"-" json:"hasSecret"`
	Enabled          bool       `db:"enabled" json:"enabled"`
	CreatedAt        time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updatedAt"`
}
