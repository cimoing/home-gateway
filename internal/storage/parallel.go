package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Large-file copy uses ranged I/O with a deep pipeline. go-smb2 caps each
	// SMB READ/WRITE at 1 MiB, so buffer size matches that unit.
	parallelCopyMinSize   = 8 << 20 // 8 MiB
	parallelCopyWorkers   = 16
	parallelCopyChunkSize = 1 << 20 // 1 MiB (= one SMB Large MTU write)
	copyBufferSize        = 1 << 20
)

var errParallelUnsupported = errors.New("parallel copy unsupported")

// parallelTransferBackend enables multi-connection range copy for large files.
// SMB backends open an independent SMB3 session per handle.
type parallelTransferBackend interface {
	PrepareParallelWrite(ctx context.Context, filePath string, size int64) error
	OpenParallelReader(ctx context.Context, filePath string) (io.ReaderAt, io.Closer, error)
	OpenParallelWriter(ctx context.Context, filePath string) (io.WriterAt, io.Closer, error)
}

func canParallelCopy(src, dst Backend) bool {
	_, okSrc := src.(parallelTransferBackend)
	_, okDst := dst.(parallelTransferBackend)
	return okSrc && okDst
}

type parallelChunk struct {
	offset int64
	length int64
}

func copyParallel(
	ctx context.Context,
	src Backend,
	dst Backend,
	sourcePath string,
	destPath string,
	size int64,
	job *syncJob,
) error {
	srcP, okSrc := src.(parallelTransferBackend)
	dstP, okDst := dst.(parallelTransferBackend)
	if !okSrc || !okDst || size < parallelCopyMinSize {
		return errParallelUnsupported
	}
	if err := dstP.PrepareParallelWrite(ctx, destPath, size); err != nil {
		return err
	}

	workers := parallelCopyWorkers
	if size < parallelCopyChunkSize*int64(workers) {
		workers = int((size + parallelCopyChunkSize - 1) / parallelCopyChunkSize)
		if workers < 1 {
			workers = 1
		}
		if workers > parallelCopyWorkers {
			workers = parallelCopyWorkers
		}
	}

	// Prefer one shared ReaderAt/WriterAt per side. For SMB this means a single
	// session with concurrent ReadAt/WriteAt pipelining (go-smb2 allows multiple
	// outstanding requests on one TCP connection). Multi-session writers to the
	// same NAS file are often serialized and stick around ~single-stream speed.
	reader, readerCloser, err := srcP.OpenParallelReader(ctx, sourcePath)
	if err != nil {
		return err
	}
	defer readerCloser.Close()

	writer, writerCloser, err := dstP.OpenParallelWriter(ctx, destPath)
	if err != nil {
		return err
	}
	defer writerCloser.Close()

	log.Printf(
		"storage copy parallel src=%s dst=%s size=%s workers=%d chunk=%s mode=pipeline",
		sourcePath, destPath, formatByteSize(size), workers,
		formatByteSize(parallelCopyChunkSize),
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chunks := make(chan parallelChunk, workers*2)
	var wg sync.WaitGroup
	errOnce := sync.Once{}
	var copyErr error
	fail := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			copyErr = err
			cancel()
		})
	}

	var transferred atomic.Int64
	started := time.Now()
	stopRateLog := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var previous int64
		previousAt := started
		for {
			select {
			case <-stopRateLog:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				current := transferred.Load()
				elapsed := now.Sub(previousAt).Seconds()
				if elapsed <= 0 {
					continue
				}
				delta := current - previous
				log.Printf(
					"storage copy parallel progress src=%s dst=%s transferred=%s rate=%s/s workers=%d",
					sourcePath, destPath, formatByteSize(current),
					formatByteSize(int64(float64(delta)/elapsed)), workers,
				)
				previous = current
				previousAt = now
			}
		}
	}()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, copyBufferSize)
			for chunk := range chunks {
				if ctx.Err() != nil {
					return
				}
				if err := copyChunkAt(ctx, reader, writer, buf, chunk.offset, chunk.length, job, &transferred); err != nil {
					fail(err)
					return
				}
			}
		}()
	}

	for offset := int64(0); offset < size; {
		if ctx.Err() != nil {
			break
		}
		length := int64(parallelCopyChunkSize)
		if offset+length > size {
			length = size - offset
		}
		select {
		case <-ctx.Done():
		case chunks <- parallelChunk{offset: offset, length: length}:
			offset += length
		}
	}
	close(chunks)
	wg.Wait()
	close(stopRateLog)

	elapsed := time.Since(started).Seconds()
	total := transferred.Load()
	if elapsed > 0 {
		log.Printf(
			"storage copy parallel done src=%s dst=%s transferred=%s elapsed=%s avg_rate=%s/s",
			sourcePath, destPath, formatByteSize(total),
			time.Since(started).Round(time.Millisecond),
			formatByteSize(int64(float64(total)/elapsed)),
		)
	}
	if copyErr != nil {
		return copyErr
	}
	return ctx.Err()
}

func copyChunkAt(
	ctx context.Context,
	reader io.ReaderAt,
	writer io.WriterAt,
	buf []byte,
	offset int64,
	length int64,
	job *syncJob,
	transferred *atomic.Int64,
) error {
	remaining := length
	pos := offset
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int(remaining)
		if n > len(buf) {
			n = len(buf)
		}
		if err := readExactAt(reader, buf[:n], pos); err != nil {
			return fmt.Errorf("read at %d: %w", pos, err)
		}
		if err := writeExactAt(writer, buf[:n], pos); err != nil {
			return fmt.Errorf("write at %d: %w", pos, err)
		}
		if transferred != nil {
			transferred.Add(int64(n))
		}
		if job != nil {
			job.addBytes(int64(n))
		}
		pos += int64(n)
		remaining -= int64(n)
	}
	return nil
}

func readExactAt(r io.ReaderAt, buf []byte, off int64) error {
	total := 0
	for total < len(buf) {
		n, err := r.ReadAt(buf[total:], off+int64(total))
		total += n
		if err != nil {
			if errors.Is(err, io.EOF) {
				if total == len(buf) {
					return nil
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}
	return nil
}

func writeExactAt(w io.WriterAt, buf []byte, off int64) error {
	total := 0
	for total < len(buf) {
		n, err := w.WriteAt(buf[total:], off+int64(total))
		total += n
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
