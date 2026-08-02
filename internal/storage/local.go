package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type localBackend struct {
	root string
}

func newLocalBackend(root string) (*localBackend, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, fmt.Errorf("%w: local root is required", ErrInvalidInput)
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: local root must be absolute", ErrInvalidInput)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	return &localBackend{root: absolute}, nil
}

func (b *localBackend) Ping(context.Context) error {
	info, err := os.Stat(b.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("local storage root is not a directory")
	}
	probe, err := os.CreateTemp(b.root, ".storage-write-test-*")
	if err != nil {
		return fmt.Errorf("local storage root is not writable: %w", err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func (b *localBackend) List(_ context.Context, dir string) ([]Entry, error) {
	absolute, err := b.resolve(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	relative, _ := cleanRelativePath(dir)
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		child := entry.Name()
		if relative != "" {
			child = relative + "/" + entry.Name()
		}
		result = append(result, Entry{
			Name:    entry.Name(),
			Path:    child,
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
	}
	return result, nil
}

func (b *localBackend) Mkdir(_ context.Context, dir string) error {
	absolute, err := b.resolve(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return err
	}
	return nil
}

func (b *localBackend) Remove(_ context.Context, target string, recursive bool) error {
	absolute, err := b.resolve(target)
	if err != nil {
		return err
	}
	if absolute == b.root {
		return fmt.Errorf("%w: refusing to remove storage root", ErrInvalidInput)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.IsDir() && !recursive {
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return ErrNotEmpty
		}
	}
	if recursive {
		return os.RemoveAll(absolute)
	}
	return os.Remove(absolute)
}

func (b *localBackend) Rename(_ context.Context, from string, to string) error {
	src, err := b.resolve(from)
	if err != nil {
		return err
	}
	dst, err := b.resolve(to)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrConflict
	}
	return os.Rename(src, dst)
}

func (b *localBackend) Open(_ context.Context, filePath string) (io.ReadCloser, error) {
	absolute, err := b.resolve(filePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: path is a directory", ErrInvalidInput)
	}
	return file, nil
}

func (b *localBackend) Create(_ context.Context, filePath string) (io.WriteCloser, error) {
	absolute, err := b.resolve(filePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
		return nil, err
	}
	return os.OpenFile(absolute, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
}

func (b *localBackend) Stat(_ context.Context, target string) (Entry, error) {
	absolute, err := b.resolve(target)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	relative, _ := cleanRelativePath(target)
	return Entry{
		Name:    info.Name(),
		Path:    relative,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}, nil
}

func (b *localBackend) Close() error { return nil }

func (b *localBackend) resolve(relative string) (string, error) {
	cleaned, err := cleanRelativePath(relative)
	if err != nil {
		return "", err
	}
	target := b.root
	if cleaned != "" {
		target = filepath.Join(b.root, filepath.FromSlash(cleaned))
	}
	target = filepath.Clean(target)
	if !isWithinRoot(b.root, target) {
		return "", ErrInvalidInput
	}
	return target, nil
}

func isWithinRoot(root string, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
