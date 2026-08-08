package bt

import (
	"context"
	"sync"
	"testing"
	"time"

	appconfig "home-gateway/internal/config"
)

type fakeEngine struct {
	mu              sync.Mutex
	nextID          int64
	tasks           map[string]*fakeEngineTask
	closed          bool
	appliedDownload int64
	appliedUpload   int64
	appliedSeed     float64
	applyCalls      int
	applyErr        error
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{nextID: 1, tasks: make(map[string]*fakeEngineTask)}
}

func (e *fakeEngine) AddMagnet(_ string, savePath string) (EngineTask, error) {
	return e.add("hash-magnet", savePath)
}

func (e *fakeEngine) AddTorrent(_ []byte, savePath string) (EngineTask, error) {
	return e.add("hash-torrent", savePath)
}

func (e *fakeEngine) add(hash, savePath string) (EngineTask, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.tasks[hash]; exists {
		return nil, ErrConflict
	}
	ready := make(chan struct{})
	close(ready)
	id := e.nextID
	e.nextID++
	task := &fakeEngineTask{
		id:       id,
		hash:     hash,
		savePath: savePath,
		ready:    ready,
		metadata: TaskMetadata{
			Name: "bundle", TotalBytes: 12,
			Files: []TaskFile{
				{Index: 0, Path: "bundle/a.txt", Length: 5},
				{Index: 1, Path: "bundle/b.txt", Length: 7},
			},
		},
		stats: TaskStats{CompletedBytes: 0, FileCompleted: map[int]int64{}},
	}
	e.tasks[hash] = task
	return task, nil
}

func (e *fakeEngine) Task(hash string) (EngineTask, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	task, ok := e.tasks[hash]
	return task, ok
}

func (e *fakeEngine) TaskByID(id int64) (EngineTask, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, task := range e.tasks {
		if task.id == id {
			return task, true
		}
	}
	return nil, false
}

func (e *fakeEngine) Remove(hash string) error {
	return e.RemoveByID(e.tasks[hash].id, false)
}

func (e *fakeEngine) RemoveByID(id int64, _ bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for hash, task := range e.tasks {
		if task.id == id {
			delete(e.tasks, hash)
			return nil
		}
	}
	return ErrNotFound
}

func (e *fakeEngine) ListRemote() ([]RemoteTorrent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]RemoteTorrent, 0, len(e.tasks))
	for _, task := range e.tasks {
		out = append(out, e.snapshotLocked(task))
	}
	return out, nil
}

func (e *fakeEngine) GetRemote(id int64) (RemoteTorrent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, task := range e.tasks {
		if task.id == id {
			return e.snapshotLocked(task), nil
		}
	}
	return RemoteTorrent{}, ErrNotFound
}

func (e *fakeEngine) snapshotLocked(task *fakeEngineTask) RemoteTorrent {
	status := "downloading"
	desired := "downloading"
	if task.paused {
		status = "paused"
		desired = "paused"
	}
	files := make([]RemoteFile, 0, len(task.metadata.Files))
	for _, file := range task.metadata.Files {
		priority := 1
		selected := true
		for _, selection := range task.selections {
			if selection.Index == file.Index {
				priority = selection.Priority
				selected = selection.Priority > 0
			}
		}
		files = append(files, RemoteFile{
			Index: file.Index, Path: file.Path, Length: file.Length,
			Selected: selected, Priority: priority,
		})
	}
	return RemoteTorrent{
		ID: task.id, InfoHash: task.hash, Name: task.metadata.Name,
		SavePath: task.savePath, Status: status, DesiredState: desired,
		TotalBytes: task.metadata.TotalBytes, CompletedBytes: task.stats.CompletedBytes,
		MetadataComplete: true, AddedAt: time.Now().UTC(), Files: files,
	}
}

func (e *fakeEngine) MagnetLink(id int64) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, task := range e.tasks {
		if task.id == id {
			return "magnet:?xt=urn:btih:" + task.hash, nil
		}
	}
	return "", ErrNotFound
}
func (e *fakeEngine) Stats() EngineStats { return EngineStats{} }
func (e *fakeEngine) SessionSettings() (SessionSettings, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return SessionSettings{
		DownloadDir:      "/downloads",
		ListenPort:       51413,
		DownloadLimitBps: e.appliedDownload,
		UploadLimitBps:   e.appliedUpload,
		SeedRatioLimit:   e.appliedSeed,
	}, nil
}
func (e *fakeEngine) ApplySessionLimits(download, upload int64, seed float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.applyCalls++
	e.appliedDownload = download
	e.appliedUpload = upload
	e.appliedSeed = seed
	return e.applyErr
}
func (e *fakeEngine) SetBlockConfig(BlockConfig) error { return nil }
func (e *fakeEngine) Close() error {
	e.closed = true
	return nil
}

type fakeEngineTask struct {
	id           int64
	hash         string
	savePath     string
	ready        chan struct{}
	metadata     TaskMetadata
	stats        TaskStats
	paused       bool
	uploadPaused bool
	selections   []FileSelection
}

func (t *fakeEngineTask) ID() int64                      { return t.id }
func (t *fakeEngineTask) InfoHash() string               { return t.hash }
func (t *fakeEngineTask) MetadataReady() <-chan struct{} { return t.ready }
func (t *fakeEngineTask) Metadata() TaskMetadata         { return t.metadata }
func (t *fakeEngineTask) Stats() TaskStats               { return t.stats }
func (t *fakeEngineTask) Peers() []PeerInfo              { return nil }
func (t *fakeEngineTask) Pause() {
	t.paused = true
	t.uploadPaused = true
}
func (t *fakeEngineTask) Resume() {
	t.paused = false
	t.uploadPaused = false
}
func (t *fakeEngineTask) PauseUpload()  { t.uploadPaused = true }
func (t *fakeEngineTask) ResumeUpload() { t.uploadPaused = false }
func (t *fakeEngineTask) SetFiles(files []FileSelection) error {
	t.selections = append([]FileSelection(nil), files...)
	return nil
}

func TestServiceRemoteTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	engine := newFakeEngine()
	service := NewService(engine, appconfig.BTConfig{
		Enable: true, DownloadDir: "/downloads", EngineDir: "/downloads", ListenPort: 51413,
	}, "")
	defer service.Close()

	task, err := service.AddMagnet(ctx, "magnet:?xt=urn:btih:abc", AddOptions{
		Subdirectory: "linux",
		Start:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == 0 || task.Name != "bundle" {
		t.Fatalf("unexpected task %+v", task)
	}
	listed, err := service.ListTasks(ctx, "", "")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list %#v err=%v", listed, err)
	}
	if _, err := service.Pause(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resume(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	files, err := service.UpdateFiles(ctx, task.ID, []FileSelection{
		{Index: 1, Priority: 0},
	})
	if err != nil || files[1].Selected {
		t.Fatalf("unexpected file selection %+v: %v", files, err)
	}
	if err := service.Delete(ctx, task.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetTask(ctx, task.ID); err != ErrNotFound {
		t.Fatalf("expected deleted task to be absent, got %v", err)
	}
}

func TestUpdateSettingsSyncsTransmission(t *testing.T) {
	engine := newFakeEngine()
	service := NewService(engine, appconfig.BTConfig{Enable: true}, "")
	defer service.Close()

	download := int64(2048 * 1024)
	upload := int64(512 * 1024)
	seed := 1.5
	settings, err := service.UpdateSettings(UpdateSettingsRequest{
		DownloadLimitBps: &download,
		UploadLimitBps:   &upload,
		SeedRatioLimit:   &seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.DownloadLimitBps != download || settings.UploadLimitBps != upload || settings.SeedRatioLimit != seed {
		t.Fatalf("unexpected settings %+v", settings)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.applyCalls != 1 ||
		engine.appliedDownload != download ||
		engine.appliedUpload != upload ||
		engine.appliedSeed != seed {
		t.Fatalf("session sync %#v", engine)
	}
}

func TestUpdateSettingsRequiresEngine(t *testing.T) {
	service := NewService(nil, appconfig.BTConfig{Enable: true}, "")
	defer service.Close()
	download := int64(1024)
	if _, err := service.UpdateSettings(UpdateSettingsRequest{DownloadLimitBps: &download}); err != ErrUnavailable {
		t.Fatalf("expected unavailable, got %v", err)
	}
}

func TestSettingsPrefersRemoteSession(t *testing.T) {
	engine := newFakeEngine()
	engine.appliedDownload = 3 * 1024
	engine.appliedUpload = 1024
	engine.appliedSeed = 2.5
	service := NewService(engine, appconfig.BTConfig{
		Enable: true, DownloadDir: "/local", EngineDir: "/local", ListenPort: 1,
		DownloadLimitBps: 0, UploadLimitBps: 0, SeedRatioLimit: 0,
	}, "")
	defer service.Close()

	settings := service.Settings()
	if settings.DownloadDir != "/downloads" || settings.ListenPort != 51413 {
		t.Fatalf("path/port %#v", settings)
	}
	if settings.DownloadLimitBps != 3*1024 || settings.UploadLimitBps != 1024 || settings.SeedRatioLimit != 2.5 {
		t.Fatalf("limits %#v", settings)
	}
}
