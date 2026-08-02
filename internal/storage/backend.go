package storage

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("storage item not found")
	ErrConflict     = errors.New("storage item already exists")
	ErrInvalidInput = errors.New("invalid storage input")
	ErrUnavailable  = errors.New("storage backend unavailable")
	ErrNotEmpty     = errors.New("directory is not empty")
)

// Entry describes one filesystem or object-store listing item.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Backend is a unified storage adapter.
type Backend interface {
	Ping(ctx context.Context) error
	List(ctx context.Context, dir string) ([]Entry, error)
	Mkdir(ctx context.Context, dir string) error
	Remove(ctx context.Context, target string, recursive bool) error
	Rename(ctx context.Context, from string, to string) error
	Open(ctx context.Context, filePath string) (io.ReadCloser, error)
	Create(ctx context.Context, filePath string) (io.WriteCloser, error)
	Stat(ctx context.Context, target string) (Entry, error)
	Close() error
}

// cleanRelativePath normalizes a POSIX-style relative path and rejects escapes.
func cleanRelativePath(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return "", nil
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", ErrInvalidInput
		}
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(raw, "/"))
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidInput
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." || part == "" {
			return "", ErrInvalidInput
		}
	}
	return cleaned, nil
}

func joinRelative(parts ...string) (string, error) {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned, err := cleanRelativePath(part)
		if err != nil {
			return "", err
		}
		if cleaned != "" {
			segments = append(segments, cleaned)
		}
	}
	return strings.Join(segments, "/"), nil
}

func baseName(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}
