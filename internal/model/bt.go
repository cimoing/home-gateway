package model

import "time"

const (
	BTStateMetadata    = "metadata"
	BTStateDownloading = "downloading"
	BTStatePaused      = "paused"
	BTStateCompleted   = "completed"
	BTStateError       = "error"
)

// BTTask stores restart-safe BitTorrent task intent and metadata.
type BTTask struct {
	ID             int64      `db:"id" json:"id"`
	InfoHash       string     `db:"info_hash" json:"infoHash"`
	SourceType     string     `db:"source_type" json:"sourceType"`
	SourceValue    string     `db:"source_value" json:"-"`
	Metainfo       []byte     `db:"metainfo" json:"-"`
	Name           string     `db:"name" json:"name"`
	SavePath       string     `db:"save_path" json:"-"`
	SaveSubdir     string     `db:"-" json:"saveSubdir"`
	DesiredState   string     `db:"desired_state" json:"desiredState"`
	Status         string     `db:"status" json:"status"`
	ErrorMessage   string     `db:"error_message" json:"error,omitempty"`
	TotalBytes     int64      `db:"total_bytes" json:"totalBytes"`
	CompletedBytes int64      `db:"-" json:"completedBytes"`
	DownloadRate   int64      `db:"-" json:"downloadRate"`
	UploadRate     int64      `db:"-" json:"uploadRate"`
	UploadedBytes  int64      `db:"-" json:"uploadedBytes"`
	Peers          int        `db:"-" json:"peers"`
	Ratio          float64    `db:"-" json:"ratio"`
	SeedingPaused  bool       `db:"-" json:"seedingPaused"`
	ETASeconds     *int64     `db:"-" json:"etaSeconds,omitempty"`
	CompletedAt    *time.Time `db:"completed_at" json:"completedAt,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updatedAt"`
}

// BTTaskFile stores the user's download selection for one torrent file.
type BTTaskFile struct {
	ID             int64  `db:"id" json:"id"`
	TaskID         int64  `db:"task_id" json:"taskId"`
	FileIndex      int    `db:"file_index" json:"index"`
	Path           string `db:"path" json:"path"`
	Length         int64  `db:"length" json:"length"`
	Selected       bool   `db:"selected" json:"selected"`
	Priority       int    `db:"priority" json:"priority"`
	CompletedBytes int64  `db:"-" json:"completedBytes"`
}
