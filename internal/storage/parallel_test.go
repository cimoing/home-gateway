package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"home-gateway/internal/config"
)

func TestCopyParallelLocalLargeFile(t *testing.T) {
	root := t.TempDir()
	srcRoot := filepath.Join(root, "src")
	dstRoot := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dstRoot, 0o750); err != nil {
		t.Fatal(err)
	}

	// Keep the fixture above the parallel threshold but small enough for CI.
	size := parallelCopyMinSize + (2 << 20)
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "big.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	src, err := OpenFromConfig(config.StorageBackendConfig{
		Name: "src", Type: "local", Config: map[string]any{"root": srcRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := OpenFromConfig(config.StorageBackendConfig{
		Name: "dst", Type: "local", Config: map[string]any{"root": dstRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()

	job := &syncJob{}
	if err := copyOneFile(
		context.Background(), nil, src, dst, "src", "dst",
		"big.bin", "out/big.bin", int64(size), true, job,
	); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dstRoot, "out", "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch: got %d bytes", len(got))
	}
	if job.copiedBytes != int64(size) {
		t.Fatalf("copiedBytes=%d want %d", job.copiedBytes, size)
	}
}

func TestCanParallelCopyRequiresBothSides(t *testing.T) {
	root := t.TempDir()
	local, err := OpenFromConfig(config.StorageBackendConfig{
		Name: "local", Type: "local", Config: map[string]any{"root": root},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if !canParallelCopy(local, local) {
		t.Fatal("expected local/local parallel support")
	}
}
