package storage

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestSMBStreamWriterHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &smbStreamWriter{
		ctx:     ctx,
		backend: &smbBackend{},
		closed:  false,
		file:    nil,
	}
	cancel()
	_, err := writer.Write([]byte("data"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write error = %v, want context.Canceled", err)
	}
	if !writer.failed {
		t.Fatal("expected writer to be marked failed after cancel")
	}
}

func TestSMBStreamWriterRejectsWriteAfterClose(t *testing.T) {
	writer := &smbStreamWriter{
		ctx:    context.Background(),
		closed: true,
	}
	if _, err := writer.Write([]byte("x")); err == nil {
		t.Fatal("expected write after close to fail")
	}
}

func TestSMBStreamWriterCloseIdempotent(t *testing.T) {
	writer := &smbStreamWriter{
		ctx:    context.Background(),
		closed: false,
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

// Ensure cancelled context surfaces through Close even without an open session.
func TestSMBStreamWriterCloseReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &smbStreamWriter{ctx: ctx, failed: true}
	if err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v, want context.Canceled", err)
	}
}

func TestProgressMatchesChunkedCopyPattern(t *testing.T) {
	// Mirrors sync path: io.Copy from local reader into a streaming writer.
	src := &slowReader{data: make([]byte, 128*1024), chunk: 32 * 1024}
	var written int
	dst := &countingWriter{onWrite: func(n int) { written += n }}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if written != len(src.data) {
		t.Fatalf("written=%d want=%d", written, len(src.data))
	}
}

type slowReader struct {
	data  []byte
	off   int
	chunk int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	end := r.off + r.chunk
	if end > len(r.data) {
		end = len(r.data)
	}
	n := copy(p, r.data[r.off:end])
	r.off += n
	time.Sleep(time.Millisecond)
	return n, nil
}

type countingWriter struct {
	onWrite func(int)
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite(len(p))
	}
	return len(p), nil
}
