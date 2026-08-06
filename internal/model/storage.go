package model

const (
	StorageTypeLocal = "local"
	StorageTypeSMB   = "smb"
	StorageTypeS3    = "s3"

	// BTSyncNone is written to legacy bt_* sync columns (feature removed).
	BTSyncNone = "none"
)

// StorageBackend is a configured storage destination (from YAML).
type StorageBackend struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	HasSecret bool   `json:"hasSecret"`
	Enabled   bool   `json:"enabled"`
}
