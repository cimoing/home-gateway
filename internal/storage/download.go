package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

func contentTypeForPath(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	if ext == "" {
		return "application/octet-stream"
	}
	if mapped := mime.TypeByExtension(ext); mapped != "" {
		return mapped
	}
	// Common media types some environments omit from the local MIME DB.
	switch ext {
	case ".mkv":
		return "video/x-matroska"
	case ".m4v":
		return "video/x-m4v"
	case ".ts", ".mts", ".m2ts":
		return "video/mp2t"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func contentDisposition(filePath string, inline bool) string {
	name := path.Base(filePath)
	kind := "attachment"
	if inline {
		kind = "inline"
	}
	return fmt.Sprintf(`%s; filename="%s"`, kind, strings.ReplaceAll(name, `"`, ``))
}

type readSeekCloser struct {
	*io.SectionReader
	closer io.Closer
}

func (r *readSeekCloser) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func openSeekableReader(ctx context.Context, backend Backend, filePath string, size int64) (io.ReadSeekCloser, error) {
	if size < 0 {
		return nil, fmt.Errorf("%w: invalid size", ErrInvalidInput)
	}
	reader, err := backend.Open(ctx, filePath)
	if err != nil {
		return nil, err
	}
	if seeker, ok := reader.(io.ReadSeekCloser); ok {
		return seeker, nil
	}
	_ = reader.Close()

	parallel, ok := backend.(parallelTransferBackend)
	if !ok {
		return nil, fmt.Errorf("%w: ranged download unsupported", ErrUnavailable)
	}
	ra, closer, err := parallel.OpenParallelReader(ctx, filePath)
	if err != nil {
		return nil, err
	}
	return &readSeekCloser{
		SectionReader: io.NewSectionReader(ra, 0, size),
		closer:        closer,
	}, nil
}

func serveFileDownload(
	w http.ResponseWriter,
	r *http.Request,
	backend Backend,
	filePath string,
	entry Entry,
	inline bool,
) error {
	if entry.IsDir {
		return fmt.Errorf("%w: path is a directory", ErrInvalidInput)
	}
	w.Header().Set("Content-Type", contentTypeForPath(filePath))
	w.Header().Set("Content-Disposition", contentDisposition(filePath, inline))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	modTime := entry.ModTime
	if modTime.IsZero() {
		modTime = time.Now().UTC()
	}

	seeker, err := openSeekableReader(r.Context(), backend, filePath, entry.Size)
	if err == nil {
		defer seeker.Close()
		http.ServeContent(w, r, path.Base(filePath), modTime, seeker)
		return nil
	}

	// Fallback: whole-object stream (no Range). Still useful for images/PDF.
	reader, openErr := backend.Open(r.Context(), filePath)
	if openErr != nil {
		return openErr
	}
	defer reader.Close()
	if entry.Size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", entry.Size))
	}
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, reader)
	return copyErr
}
