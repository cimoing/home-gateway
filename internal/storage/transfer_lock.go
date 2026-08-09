package storage

import (
	"strings"
	"sync"
)

// transferLock prevents the same source→dest file pair from being copied
// concurrently by overlapping sync jobs or schedules.
type transferLock struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func newTransferLock() *transferLock {
	return &transferLock{active: make(map[string]struct{})}
}

func syncTransferKey(srcBackend, srcPath, dstBackend, dstPath string) string {
	return strings.Join([]string{
		strings.TrimSpace(srcBackend),
		strings.TrimSpace(srcPath),
		strings.TrimSpace(dstBackend),
		strings.TrimSpace(dstPath),
	}, "\x00")
}

// TryBegin reserves a transfer. ok=false means the same pair is already in flight.
func (l *transferLock) TryBegin(srcBackend, srcPath, dstBackend, dstPath string) (release func(), ok bool) {
	if l == nil {
		return func() {}, true
	}
	key := syncTransferKey(srcBackend, srcPath, dstBackend, dstPath)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.active[key]; exists {
		return nil, false
	}
	l.active[key] = struct{}{}
	return func() {
		l.mu.Lock()
		delete(l.active, key)
		l.mu.Unlock()
	}, true
}
