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
	Stats() EngineStats
	SetRateLimits(downloadBps, uploadBps int64)
	Close() error
}

// EngineTask is the runtime control surface used by Service.
type EngineTask interface {
	InfoHash() string
	MetadataReady() <-chan struct{}
	Metadata() TaskMetadata
	Stats() TaskStats
	Peers() []PeerInfo
	Pause()
	Resume()
	PauseUpload()
	ResumeUpload()
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
// DownloadedBytes/UploadedBytes are torrent file payload only (piece data),
// never wire/protocol chatter such as handshake, bitfield, have, or keepalive.
type TaskStats struct {
	CompletedBytes  int64
	DownloadedBytes int64
	UploadedBytes   int64
	ActivePeers     int
	FileCompleted   map[int]int64
}

// PeerInfo describes one connected peer for a task.
// Downloaded/Uploaded and rates are file payload only.
type PeerInfo struct {
	Address      string `json:"address"`
	PeerID       string `json:"peerId"`
	Network      string `json:"network"`
	Source       string `json:"source"`
	Downloaded   int64  `json:"downloadedBytes"`
	Uploaded     int64  `json:"uploadedBytes"`
	DownloadRate int64  `json:"downloadRate"`
	UploadRate   int64  `json:"uploadRate"`
}

// EngineStats contains process-wide BitTorrent gauges.
// DownloadedBytes/UploadedBytes are file payload only across all torrents.
type EngineStats struct {
	DHTNodes        int
	DHTGoodNodes    int
	DownloadedBytes int64
	UploadedBytes   int64
}
