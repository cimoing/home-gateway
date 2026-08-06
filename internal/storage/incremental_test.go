package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"home-gateway/internal/config"
)

func TestSyncIncrementalCopiesChangedFilesOnly(t *testing.T) {
	root := t.TempDir()
	srcRoot := filepath.Join(root, "src")
	dstRoot := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(srcRoot, "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dstRoot, "backup", "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	samePath := filepath.Join(srcRoot, "a", "same.txt")
	changedPath := filepath.Join(srcRoot, "a", "changed.txt")
	newPath := filepath.Join(srcRoot, "a", "new.txt")
	if err := os.WriteFile(samePath, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, []byte("new-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("brand"), 0o600); err != nil {
		t.Fatal(err)
	}
	dstSame := filepath.Join(dstRoot, "backup", "a", "same.txt")
	dstChanged := filepath.Join(dstRoot, "backup", "a", "changed.txt")
	if err := os.WriteFile(dstSame, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstChanged, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(dstSame, old, old)
	_ = os.Chtimes(samePath, old, old)

	service := NewService([]config.StorageBackendConfig{
		{Name: "src", Type: "local", Config: map[string]any{"root": srcRoot}},
		{Name: "dst", Type: "local", Config: map[string]any{"root": dstRoot}},
	})
	result, err := service.SyncIncremental(context.Background(), config.StorageSyncRule{
		Interval: "0 * * * *",
		Src:      config.StorageSyncEndpoint{Name: "src", Path: ""},
		Dst:      config.StorageSyncEndpoint{Name: "dst", Path: "backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 3 || result.Copied != 2 || result.Skipped != 1 {
		t.Fatalf("unexpected result %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(dstRoot, "backup", "a", "new.txt"))
	if err != nil || string(data) != "brand" {
		t.Fatalf("new file %q err=%v", data, err)
	}
	data, err = os.ReadFile(dstChanged)
	if err != nil || string(data) != "new-content" {
		t.Fatalf("changed file %q err=%v", data, err)
	}
}

func TestNeedsIncrementalCopy(t *testing.T) {
	src := listedFile{size: 10, modTime: time.Now()}
	if !needsIncrementalCopy(src, Entry{}, ErrNotFound) {
		t.Fatal("missing dest should copy")
	}
	if !needsIncrementalCopy(src, Entry{Size: 9, ModTime: time.Now()}, nil) {
		t.Fatal("size mismatch should copy")
	}
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	if !needsIncrementalCopy(
		listedFile{size: 10, modTime: newer},
		Entry{Size: 10, ModTime: older},
		nil,
	) {
		t.Fatal("newer source should copy")
	}
	if needsIncrementalCopy(
		listedFile{size: 10, modTime: older},
		Entry{Size: 10, ModTime: newer},
		nil,
	) {
		t.Fatal("unchanged file should skip")
	}
}
