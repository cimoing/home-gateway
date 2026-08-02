package model

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

// StorageBackend is a configured storage destination (from YAML).
type StorageBackend struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	HasSecret bool   `json:"hasSecret"`
	Enabled   bool   `json:"enabled"`
}
