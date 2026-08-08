package bt

import (
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("BT task not found")
	ErrConflict     = errors.New("BT task already exists")
	ErrInvalidInput = errors.New("invalid BT input")
	ErrUnavailable  = errors.New("BT 引擎尚未就绪，请稍后重试")
)

// Engine owns the BitTorrent client (Transmission RPC).
type Engine interface {
	AddMagnet(uri string, savePath string) (EngineTask, error)
	AddTorrent(metainfo []byte, savePath string) (EngineTask, error)
	Task(infoHash string) (EngineTask, bool)
	TaskByID(id int64) (EngineTask, bool)
	Remove(infoHash string) error
	RemoveByID(id int64, deleteData bool) error
	ListRemote() ([]RemoteTorrent, error)
	GetRemote(id int64) (RemoteTorrent, error)
	MagnetLink(id int64) (string, error)
	Stats() EngineStats
	SessionSettings() (SessionSettings, error)
	ApplySessionLimits(downloadBps, uploadBps int64, seedRatioLimit float64) error
	SetBlockConfig(config BlockConfig) error
	Close() error
}

// SessionSettings is the mutable Transmission session configuration shown in the UI.
type SessionSettings struct {
	DownloadDir      string
	ListenPort       int
	DownloadLimitBps int64
	UploadLimitBps   int64
	SeedRatioLimit   float64
}

// RemoteTorrent is a torrent snapshot from the remote engine.
type RemoteTorrent struct {
	ID               int64
	InfoHash         string
	Name             string
	SavePath         string
	Status           string
	DesiredState     string
	Error            string
	TotalBytes       int64
	CompletedBytes   int64
	DownloadedBytes  int64
	UploadedBytes    int64
	DesiredAvailable int64
	SizeWhenDone     int64
	PercentDone      float64
	AvailablePercent float64
	Peers            int
	MetadataComplete bool
	MagnetLink       string
	AddedAt          time.Time
	Files            []RemoteFile
}

// RemoteFile is one file entry from the remote engine.
type RemoteFile struct {
	Index          int
	Path           string
	Length         int64
	Selected       bool
	Priority       int
	CompletedBytes int64
}

// EngineTask is the runtime control surface used by Service.
type EngineTask interface {
	ID() int64
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
type TaskStats struct {
	CompletedBytes  int64
	DownloadedBytes int64
	UploadedBytes   int64
	ActivePeers     int
	FileCompleted   map[int]int64
}

// PeerInfo describes one connected peer for a task.
type PeerInfo struct {
	Address       string  `json:"address"`
	PeerID        string  `json:"peerId"`
	Client        string  `json:"client"`
	ClientVersion string  `json:"clientVersion"`
	Network       string  `json:"network"`
	Source        string  `json:"source"`
	Progress      float64 `json:"progress"`
	Downloaded    int64   `json:"downloadedBytes"`
	Uploaded      int64   `json:"uploadedBytes"`
	DownloadRate  int64   `json:"downloadRate"`
	UploadRate    int64   `json:"uploadRate"`
}

// EngineStats contains process-wide BitTorrent gauges.
type EngineStats struct {
	DHTNodes        int
	DHTGoodNodes    int
	DownloadedBytes int64
	UploadedBytes   int64
}
