package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
)

type smbConfig struct {
	Host     string
	Port     int
	Share    string
	Username string
	Domain   string
	Password string
}

type smbBackend struct {
	cfg smbConfig
}

func newSMBBackend(cfg smbConfig) (*smbBackend, error) {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Share = strings.Trim(strings.TrimSpace(cfg.Share), "/\\")
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	if cfg.Host == "" || cfg.Share == "" || cfg.Username == "" {
		return nil, fmt.Errorf("%w: smb host, share, and username are required", ErrInvalidInput)
	}
	if cfg.Port <= 0 {
		cfg.Port = 445
	}
	if cfg.Port > 65535 {
		return nil, fmt.Errorf("%w: invalid smb port", ErrInvalidInput)
	}
	return &smbBackend{cfg: cfg}, nil
}

func (b *smbBackend) withShare(ctx context.Context, fn func(*smb2.Share) error) error {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", b.cfg.Host, b.cfg.Port))
	if err != nil {
		return fmt.Errorf("%w: dial smb: %v", ErrUnavailable, err)
	}
	defer conn.Close()

	session, err := (&smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     b.cfg.Username,
			Password: b.cfg.Password,
			Domain:   b.cfg.Domain,
		},
	}).Dial(conn)
	if err != nil {
		return fmt.Errorf("%w: smb login: %v", ErrUnavailable, err)
	}
	defer session.Logoff()

	share, err := session.Mount(b.cfg.Share)
	if err != nil {
		return fmt.Errorf("%w: mount smb share: %v", ErrUnavailable, err)
	}
	defer share.Umount()
	return fn(share)
}

func (b *smbBackend) Ping(ctx context.Context) error {
	return b.withShare(ctx, func(share *smb2.Share) error {
		_, err := share.ReadDir(".")
		return err
	})
}

func (b *smbBackend) List(ctx context.Context, dir string) ([]Entry, error) {
	cleaned, err := cleanRelativePath(dir)
	if err != nil {
		return nil, err
	}
	target := "."
	if cleaned != "" {
		target = cleaned
	}
	var result []Entry
	err = b.withShare(ctx, func(share *smb2.Share) error {
		entries, err := share.ReadDir(target)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		result = make([]Entry, 0, len(entries))
		for _, entry := range entries {
			child := entry.Name()
			if cleaned != "" {
				child = cleaned + "/" + entry.Name()
			}
			result = append(result, Entry{
				Name:    entry.Name(),
				Path:    child,
				IsDir:   entry.IsDir(),
				Size:    entry.Size(),
				ModTime: entry.ModTime().UTC(),
			})
		}
		return nil
	})
	return result, err
}

func (b *smbBackend) Mkdir(ctx context.Context, dir string) error {
	cleaned, err := cleanRelativePath(dir)
	if err != nil || cleaned == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: directory path is required", ErrInvalidInput)
	}
	return b.withShare(ctx, func(share *smb2.Share) error {
		return mkdirAllSMB(share, cleaned)
	})
}

func (b *smbBackend) Remove(ctx context.Context, target string, recursive bool) error {
	cleaned, err := cleanRelativePath(target)
	if err != nil || cleaned == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: refusing to remove share root", ErrInvalidInput)
	}
	return b.withShare(ctx, func(share *smb2.Share) error {
		info, err := share.Stat(cleaned)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		if info.IsDir() {
			if recursive {
				return removeAllSMB(share, cleaned)
			}
			entries, err := share.ReadDir(cleaned)
			if err != nil {
				return err
			}
			if len(entries) > 0 {
				return ErrNotEmpty
			}
			return share.Remove(cleaned)
		}
		return share.Remove(cleaned)
	})
}

func (b *smbBackend) Rename(ctx context.Context, from string, to string) error {
	src, err := cleanRelativePath(from)
	if err != nil || src == "" {
		return fmt.Errorf("%w: invalid source path", ErrInvalidInput)
	}
	dst, err := cleanRelativePath(to)
	if err != nil || dst == "" {
		return fmt.Errorf("%w: invalid destination path", ErrInvalidInput)
	}
	return b.withShare(ctx, func(share *smb2.Share) error {
		if _, err := share.Stat(dst); err == nil {
			return ErrConflict
		}
		if dir := path.Dir(dst); dir != "." {
			if err := mkdirAllSMB(share, dir); err != nil {
				return err
			}
		}
		return share.Rename(src, dst)
	})
}

func (b *smbBackend) Open(ctx context.Context, filePath string) (io.ReadCloser, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil || cleaned == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	var data []byte
	err = b.withShare(ctx, func(share *smb2.Share) error {
		file, err := share.Open(cleaned)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("%w: path is a directory", ErrInvalidInput)
		}
		data, err = io.ReadAll(file)
		return err
	})
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *smbBackend) Create(ctx context.Context, filePath string) (io.WriteCloser, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil || cleaned == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	return &smbDeferredWriter{backend: b, path: cleaned}, nil
}

func (b *smbBackend) Stat(ctx context.Context, target string) (Entry, error) {
	cleaned, err := cleanRelativePath(target)
	if err != nil {
		return Entry{}, err
	}
	statPath := "."
	if cleaned != "" {
		statPath = cleaned
	}
	var entry Entry
	err = b.withShare(ctx, func(share *smb2.Share) error {
		info, err := share.Stat(statPath)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		entry = Entry{
			Name:    info.Name(),
			Path:    cleaned,
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		}
		return nil
	})
	return entry, err
}

func (b *smbBackend) Close() error { return nil }

type smbDeferredWriter struct {
	backend *smbBackend
	path    string
	buf     bytes.Buffer
	closed  bool
}

func (w *smbDeferredWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("writer closed")
	}
	return w.buf.Write(p)
}

func (w *smbDeferredWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	data := w.buf.Bytes()
	return w.backend.withShare(context.Background(), func(share *smb2.Share) error {
		if dir := path.Dir(w.path); dir != "." {
			if err := mkdirAllSMB(share, dir); err != nil {
				return err
			}
		}
		file, err := share.Create(w.path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(data)
		return err
	})
}

func mkdirAllSMB(share *smb2.Share, dir string) error {
	dir = strings.Trim(dir, "/")
	if dir == "" || dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		err := share.Mkdir(current, 0o755)
		if err != nil && !os.IsExist(err) {
			if info, statErr := share.Stat(current); statErr == nil && info.IsDir() {
				continue
			}
			return err
		}
	}
	return nil
}

func removeAllSMB(share *smb2.Share, target string) error {
	info, err := share.Stat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return share.Remove(target)
	}
	entries, err := share.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := target + "/" + entry.Name()
		if entry.IsDir() {
			if err := removeAllSMB(share, child); err != nil {
				return err
			}
		} else if err := share.Remove(child); err != nil {
			return err
		}
	}
	return share.Remove(target)
}
