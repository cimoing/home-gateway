package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"home-gateway/internal/config"
)

func TestSyncJobCopiesBetweenLocalBackends(t *testing.T) {
	root := t.TempDir()
	leftRoot := filepath.Join(root, "left")
	rightRoot := filepath.Join(root, "right")
	if err := os.MkdirAll(filepath.Join(leftRoot, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rightRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(leftRoot, "docs", "note.txt")
	if err := os.WriteFile(srcFile, []byte("hello-sync"), 0o600); err != nil {
		t.Fatal(err)
	}

	service := NewService([]config.StorageBackendConfig{
		{Name: "left", Type: "local", Config: map[string]any{"root": leftRoot}},
		{Name: "right", Type: "local", Config: map[string]any{"root": rightRoot}},
	})
	job, err := service.StartSyncJob(context.Background(), SyncJobRequest{
		SourceBackend: "left",
		DestBackend:   "right",
		Overwrite:     true,
		Items: []SyncItem{
			{SourcePath: "docs", DestPath: "backup/docs"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := service.GetSyncJob(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status == syncJobCompleted {
			break
		}
		if status.Status == syncJobFailed || status.Status == syncJobCanceled {
			t.Fatalf("job status=%s err=%s", status.Status, status.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for job: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, err := os.ReadFile(filepath.Join(rightRoot, "backup", "docs", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello-sync" {
		t.Fatalf("copied content %q", data)
	}
}

func TestEnsureDirAndCopyFileHelpers(t *testing.T) {
	root := t.TempDir()
	backend, err := OpenFromConfig(config.StorageBackendConfig{
		Name: "local", Type: "local", Config: map[string]any{"root": root},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	ctx := context.Background()
	if err := ensureDir(ctx, backend, "a/b"); err != nil {
		t.Fatal(err)
	}
	writer, err := backend.Create(ctx, "a/b/c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "x"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	entry, err := backend.Stat(ctx, "a/b/c.txt")
	if err != nil || entry.IsDir || entry.Size != 1 {
		t.Fatalf("stat %#v err=%v", entry, err)
	}
}
