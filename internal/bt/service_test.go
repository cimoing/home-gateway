package bt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appconfig "home-gateway/internal/config"
	"home-gateway/internal/database"
)

type fakeEngine struct {
	mu     sync.Mutex
	tasks  map[string]*fakeEngineTask
	closed bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{tasks: make(map[string]*fakeEngineTask)}
}

func (e *fakeEngine) AddMagnet(_ string, _ string) (EngineTask, error) {
	return e.add("hash-magnet")
}

func (e *fakeEngine) AddTorrent(_ []byte, _ string) (EngineTask, error) {
	return e.add("hash-torrent")
}

func (e *fakeEngine) add(hash string) (EngineTask, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.tasks[hash]; exists {
		return nil, ErrConflict
	}
	ready := make(chan struct{})
	close(ready)
	task := &fakeEngineTask{
		hash:  hash,
		ready: ready,
		metadata: TaskMetadata{
			Name: "bundle", TotalBytes: 12,
			Files: []TaskFile{
				{Index: 0, Path: "bundle/a.txt", Length: 5},
				{Index: 1, Path: "bundle/b.txt", Length: 7},
			},
		},
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

func (e *fakeEngine) Remove(hash string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.tasks[hash]; !ok {
		return ErrNotFound
	}
	delete(e.tasks, hash)
	return nil
}

func (e *fakeEngine) Stats() EngineStats         { return EngineStats{} }
func (e *fakeEngine) SetRateLimits(int64, int64) {}
func (e *fakeEngine) Close() error {
	e.closed = true
	return nil
}

type fakeEngineTask struct {
	hash         string
	ready        chan struct{}
	metadata     TaskMetadata
	stats        TaskStats
	paused       bool
	uploadPaused bool
	selections   []FileSelection
}

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

func TestServiceTaskLifecycleAndSafeDataDelete(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, database.Config{
		Driver: database.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "bt.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, database.DriverSQLite); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	engine := newFakeEngine()
	service := NewService(db, engine, appconfig.BTConfig{
		Enabled: true, DownloadDir: root, ListenPort: 42069,
	}, "")
	defer service.Close()

	task, err := service.AddMagnet(ctx, "magnet:?xt=urn:btih:abc", AddOptions{
		Subdirectory: "linux",
		Start:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForFiles(t, service, task.ID)
	task, err = service.GetTask(ctx, task.ID)
	if err != nil || task.Name != "bundle" || task.Status != "downloading" {
		t.Fatalf("unexpected task %+v: %v", task, err)
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

	dataPath := filepath.Join(root, "linux", "bundle", "a.txt")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, task.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected downloaded file to be deleted, got %v", err)
	}
	if _, err := service.GetTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted task to be absent, got %v", err)
	}
}

func waitForFiles(t *testing.T, service *Service, taskID int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		files, err := service.Files(context.Background(), taskID)
		if err == nil && len(files) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for torrent metadata")
}
