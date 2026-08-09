package storage

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"home-gateway/internal/config"
)

func TestContentTypeForPath(t *testing.T) {
	cases := map[string]string{
		"a.mp4":  "video/mp4",
		"b.webm": "video/webm",
		"c.png":  "image/png",
		"d.pdf":  "application/pdf",
		"e.bin":  "application/octet-stream",
		"f.webp": "image/webp",
	}
	for name, want := range cases {
		if got := contentTypeForPath(name); got != want {
			t.Fatalf("%s: content-type=%q want %q", name, got, want)
		}
	}
}

func TestServeFileDownloadRangeLocal(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clip.mp4"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := OpenFromConfig(config.StorageBackendConfig{
		Name: "local", Type: "local", Config: map[string]any{"root": root},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	entry, err := backend.Stat(context.Background(), "clip.mp4")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/download?disposition=inline", nil)
	request.Header.Set("Range", "bytes=0-1023")
	recorder := httptest.NewRecorder()
	if err := serveFileDownload(recorder, request, backend, "clip.mp4", entry, true); err != nil {
		t.Fatal(err)
	}
	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusPartialContent {
		t.Fatalf("status=%d want %d", result.StatusCode, http.StatusPartialContent)
	}
	if got := result.Header.Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := result.Header.Get("Content-Disposition"); got == "" || got[:6] != "inline" {
		t.Fatalf("Content-Disposition=%q", got)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 1024 {
		t.Fatalf("body len=%d", len(body))
	}
}
