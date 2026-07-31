package bt

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// AnacrolixEngine adapts github.com/anacrolix/torrent to Engine.
type AnacrolixEngine struct {
	client *torrent.Client
	mu     sync.RWMutex
	tasks  map[string]*anacrolixTask
}

// NewAnacrolixEngine starts a process-wide BitTorrent client.
func NewAnacrolixEngine(downloadDir string, listenPort int) (*AnacrolixEngine, error) {
	config := torrent.NewDefaultClientConfig()
	config.DataDir = downloadDir
	config.ListenPort = listenPort
	client, err := torrent.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("start BitTorrent client: %w", err)
	}
	return &AnacrolixEngine{client: client, tasks: make(map[string]*anacrolixTask)}, nil
}

func (e *AnacrolixEngine) AddMagnet(uri string, savePath string) (EngineTask, error) {
	spec, err := torrent.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return nil, fmt.Errorf("%w: parse magnet URI: %v", ErrInvalidInput, err)
	}
	return e.addSpec(spec, savePath)
}

func (e *AnacrolixEngine) AddTorrent(data []byte, savePath string) (EngineTask, error) {
	meta, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: parse torrent file: %v", ErrInvalidInput, err)
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(meta)
	if err != nil {
		return nil, fmt.Errorf("%w: parse torrent metadata: %v", ErrInvalidInput, err)
	}
	return e.addSpec(spec, savePath)
}

func (e *AnacrolixEngine) addSpec(
	spec *torrent.TorrentSpec,
	savePath string,
) (EngineTask, error) {
	fileStorage := storage.NewFile(savePath)
	spec.Storage = fileStorage
	task, added, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		_ = fileStorage.Close()
		return nil, fmt.Errorf("add torrent: %w", err)
	}
	if !added {
		_ = fileStorage.Close()
		return nil, ErrConflict
	}
	wrapped := &anacrolixTask{torrent: task, storage: fileStorage}
	hash := wrapped.InfoHash()
	e.mu.Lock()
	e.tasks[hash] = wrapped
	e.mu.Unlock()
	return wrapped, nil
}

func (e *AnacrolixEngine) Task(infoHash string) (EngineTask, bool) {
	e.mu.RLock()
	task, ok := e.tasks[strings.ToLower(infoHash)]
	e.mu.RUnlock()
	return task, ok
}

func (e *AnacrolixEngine) Remove(infoHash string) error {
	hash := strings.ToLower(infoHash)
	e.mu.Lock()
	task, ok := e.tasks[hash]
	if ok {
		delete(e.tasks, hash)
	}
	e.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	task.torrent.Drop()
	if err := task.storage.Close(); err != nil {
		return fmt.Errorf("close torrent storage: %w", err)
	}
	return nil
}

func (e *AnacrolixEngine) Close() error {
	e.mu.Lock()
	tasks := e.tasks
	e.tasks = make(map[string]*anacrolixTask)
	e.mu.Unlock()
	var errs []error
	errs = append(errs, e.client.Close()...)
	for _, task := range tasks {
		if err := task.storage.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type anacrolixTask struct {
	torrent *torrent.Torrent
	storage storage.ClientImplCloser
}

func (t *anacrolixTask) InfoHash() string {
	return strings.ToLower(t.torrent.InfoHash().HexString())
}

func (t *anacrolixTask) MetadataReady() <-chan struct{} {
	return t.torrent.GotInfo()
}

func (t *anacrolixTask) Metadata() TaskMetadata {
	info := t.torrent.Info()
	if info == nil {
		return TaskMetadata{}
	}
	files := t.torrent.Files()
	result := TaskMetadata{
		Name:       info.BestName(),
		TotalBytes: t.torrent.Length(),
		Files:      make([]TaskFile, 0, len(files)),
	}
	for index, file := range files {
		result.Files = append(result.Files, TaskFile{
			Index: index, Path: file.Path(), Length: file.Length(),
		})
	}
	return result
}

func (t *anacrolixTask) Stats() TaskStats {
	stats := t.torrent.Stats()
	files := t.torrent.Files()
	fileCompleted := make(map[int]int64, len(files))
	for index, file := range files {
		fileCompleted[index] = file.BytesCompleted()
	}
	return TaskStats{
		CompletedBytes:  t.torrent.BytesCompleted(),
		DownloadedBytes: stats.BytesReadUsefulData.Int64(),
		UploadedBytes:   stats.BytesWrittenData.Int64(),
		ActivePeers:     stats.ActivePeers,
		FileCompleted:   fileCompleted,
	}
}

func (t *anacrolixTask) Pause() {
	t.torrent.DisallowDataDownload()
}

func (t *anacrolixTask) Resume() {
	t.torrent.AllowDataDownload()
}

func (t *anacrolixTask) SetFiles(selections []FileSelection) error {
	files := t.torrent.Files()
	priorities := make(map[int]int, len(selections))
	for _, selection := range selections {
		if selection.Index < 0 || selection.Index >= len(files) {
			return fmt.Errorf("%w: file index %d", ErrInvalidInput, selection.Index)
		}
		if selection.Priority < 0 || selection.Priority > 2 {
			return fmt.Errorf("%w: file priority must be 0, 1, or 2", ErrInvalidInput)
		}
		priorities[selection.Index] = selection.Priority
	}
	for index, file := range files {
		priority := priorities[index]
		switch priority {
		case 0:
			file.SetPriority(torrent.PiecePriorityNone)
		case 1:
			file.SetPriority(torrent.PiecePriorityNormal)
		case 2:
			file.SetPriority(torrent.PiecePriorityHigh)
		}
	}
	return nil
}
