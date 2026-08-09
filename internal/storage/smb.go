package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
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
	cfg     smbConfig
	mu      sync.Mutex
	session *smbSession
	leases  int
	closed  bool
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

type smbSession struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
}

func (s *smbSession) close() {
	if s == nil {
		return
	}
	if s.share != nil {
		_ = s.share.Umount()
		s.share = nil
	}
	if s.session != nil {
		_ = s.session.Logoff()
		s.session = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (b *smbBackend) dialSession(ctx context.Context) (*smbSession, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", b.cfg.Host, b.cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("%w: dial smb: %v", ErrUnavailable, err)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	session, err := (&smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     b.cfg.Username,
			Password: b.cfg.Password,
			Domain:   b.cfg.Domain,
		},
	}).Dial(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: smb login: %v", ErrUnavailable, err)
	}
	share, err := session.Mount(b.cfg.Share)
	if err != nil {
		_ = session.Logoff()
		_ = conn.Close()
		return nil, fmt.Errorf("%w: mount smb share: %v", ErrUnavailable, err)
	}
	return &smbSession{conn: conn, session: session, share: share}, nil
}

func (b *smbBackend) ensureSessionLocked(ctx context.Context) (*smbSession, error) {
	if b.closed {
		return nil, fmt.Errorf("%w: smb backend closed", ErrUnavailable)
	}
	if b.session != nil {
		return b.session, nil
	}
	session, err := b.dialSession(ctx)
	if err != nil {
		return nil, err
	}
	b.session = session
	return session, nil
}

func (b *smbBackend) invalidateSessionLocked() {
	if b.session != nil {
		b.session.close()
		b.session = nil
	}
}

func isSMBConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "use of closed") ||
		strings.Contains(msg, "session") && strings.Contains(msg, "clos")
}

func (b *smbBackend) withShare(ctx context.Context, fn func(*smb2.Share) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	session, err := b.ensureSessionLocked(ctx)
	if err != nil {
		return err
	}
	err = fn(session.share)
	if err == nil || !isSMBConnError(err) {
		return err
	}
	b.invalidateSessionLocked()
	session, retryErr := b.ensureSessionLocked(ctx)
	if retryErr != nil {
		return err
	}
	err = fn(session.share)
	if err != nil && isSMBConnError(err) {
		b.invalidateSessionLocked()
	}
	return err
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
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	session, err := b.ensureSessionLocked(ctx)
	if err != nil {
		return nil, err
	}
	file, err := session.share.Open(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		if isSMBConnError(err) {
			b.invalidateSessionLocked()
		}
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		if isSMBConnError(err) {
			b.invalidateSessionLocked()
		}
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: path is a directory", ErrInvalidInput)
	}
	b.leases++
	return &smbStreamReader{
		ctx:     ctx,
		backend: b,
		file:    file,
		path:    cleaned,
	}, nil
}

func (b *smbBackend) Create(ctx context.Context, filePath string) (io.WriteCloser, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil || cleaned == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	session, err := b.ensureSessionLocked(ctx)
	if err != nil {
		return nil, err
	}
	if dir := path.Dir(cleaned); dir != "." {
		if err := mkdirAllSMB(session.share, dir); err != nil {
			if isSMBConnError(err) {
				b.invalidateSessionLocked()
			}
			return nil, err
		}
	}
	file, err := session.share.Create(cleaned)
	if err != nil {
		if isSMBConnError(err) {
			b.invalidateSessionLocked()
		}
		return nil, err
	}
	b.leases++
	return &smbStreamWriter{
		ctx:     ctx,
		backend: b,
		file:    file,
		path:    cleaned,
	}, nil
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

func (b *smbBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.invalidateSessionLocked()
	return nil
}

func (b *smbBackend) releaseLeaseLocked() {
	if b.leases > 0 {
		b.leases--
	}
}

// smbStreamReader streams bytes from SMB without buffering the whole file.
type smbStreamReader struct {
	ctx     context.Context
	backend *smbBackend
	file    *smb2.File
	path    string
	closed  bool
}

func (r *smbStreamReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("reader closed")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.file == nil || r.backend == nil {
		return 0, errors.New("smb reader not open")
	}
	r.backend.mu.Lock()
	defer r.backend.mu.Unlock()
	if r.closed || r.file == nil {
		return 0, errors.New("reader closed")
	}
	n, err := r.file.Read(p)
	if err != nil && err != io.EOF && isSMBConnError(err) {
		r.backend.invalidateSessionLocked()
	}
	return n, err
}

func (r *smbStreamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.backend == nil {
		if r.file != nil {
			err := r.file.Close()
			r.file = nil
			return err
		}
		return nil
	}
	r.backend.mu.Lock()
	defer r.backend.mu.Unlock()
	var closeErr error
	if r.file != nil {
		closeErr = r.file.Close()
		r.file = nil
	}
	r.backend.releaseLeaseLocked()
	return closeErr
}

// smbStreamWriter streams bytes directly to SMB without buffering the whole file.
type smbStreamWriter struct {
	ctx     context.Context
	backend *smbBackend
	file    *smb2.File
	path    string
	written int64
	failed  bool
	closed  bool
}

func (w *smbStreamWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("writer closed")
	}
	if err := w.ctx.Err(); err != nil {
		w.failed = true
		return 0, err
	}
	if w.file == nil || w.backend == nil {
		return 0, errors.New("smb writer not open")
	}
	w.backend.mu.Lock()
	defer w.backend.mu.Unlock()
	if w.closed || w.file == nil {
		return 0, errors.New("writer closed")
	}
	n, err := w.file.Write(p)
	if n > 0 {
		w.written += int64(n)
	}
	if err != nil {
		w.failed = true
		if isSMBConnError(err) {
			w.backend.invalidateSessionLocked()
		}
	}
	return n, err
}

func (w *smbStreamWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if w.backend == nil {
		var closeErr error
		if w.file != nil {
			closeErr = w.file.Close()
			w.file = nil
		}
		if closeErr != nil {
			return closeErr
		}
		if err := w.ctx.Err(); err != nil {
			return err
		}
		return nil
	}

	w.backend.mu.Lock()
	defer w.backend.mu.Unlock()

	var closeErr error
	if w.file != nil {
		closeErr = w.file.Close()
		w.file = nil
		if closeErr != nil {
			w.failed = true
		}
	}
	if (w.failed || w.ctx.Err() != nil) && w.backend.session != nil && w.backend.session.share != nil {
		_ = w.backend.session.share.Remove(w.path)
	}
	w.backend.releaseLeaseLocked()
	if closeErr != nil {
		return closeErr
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	return nil
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
