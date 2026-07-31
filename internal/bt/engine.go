package bt

import (
	"errors"
)

var (
	ErrNotFound     = errors.New("BT task not found")
	ErrConflict     = errors.New("BT task already exists")
	ErrInvalidInput = errors.New("invalid BT input")
	ErrUnavailable  = errors.New("BT engine unavailable")
)

// Engine owns the process-wide BitTorrent client.
type Engine interface {
	AddMagnet(uri string, savePath string) (EngineTask, error)
	AddTorrent(metainfo []byte, savePath string) (EngineTask, error)
	Task(infoHash string) (EngineTask, bool)
	Remove(infoHash string) error
	Close() error
}

// EngineTask is the runtime control surface used by Service.
type EngineTask interface {
	InfoHash() string
	MetadataReady() <-chan struct{}
	Metadata() TaskMetadata
	Stats() TaskStats
	Pause()
	Resume()
	SetFiles([]FileSelection) error
}

// TaskMetadata is immutable after metadata becomes available.
type TaskMetadata struct {
	Name       string
	TotalBytes int64
	Files      []TaskFile
}

// TaskFile identifies one file in torrent order.
type TaskFile struct {
	Index  int
	Path   string
	Length int64
}

// FileSelection controls download priority. Zero means not selected.
type FileSelection struct {
	Index    int `json:"index"`
	Priority int `json:"priority"`
}

// TaskStats contains cumulative counters and live gauges.
type TaskStats struct {
	CompletedBytes  int64
	DownloadedBytes int64
	UploadedBytes   int64
	ActivePeers     int
	FileCompleted   map[int]int64
}
