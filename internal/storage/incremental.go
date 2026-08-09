package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"home-gateway/internal/config"
)

// IncrementalResult summarizes one scheduled or on-demand incremental sync.
type IncrementalResult struct {
	Scanned int   `json:"scanned"`
	Copied  int   `json:"copied"`
	Skipped int   `json:"skipped"`
	Bytes   int64 `json:"bytes"`
}

// SyncIncremental copies only files that are missing, differ in size, or have a
// newer source modification time under the configured endpoints.
func (s *Service) SyncIncremental(ctx context.Context, rule config.StorageSyncRule) (IncrementalResult, error) {
	srcName := strings.TrimSpace(rule.Src.Name)
	dstName := strings.TrimSpace(rule.Dst.Name)
	srcPath, err := cleanRelativePath(rule.Src.Path)
	if err != nil {
		return IncrementalResult{}, fmt.Errorf("%w: invalid src.path", ErrInvalidInput)
	}
	dstPath, err := cleanRelativePath(rule.Dst.Path)
	if err != nil {
		return IncrementalResult{}, fmt.Errorf("%w: invalid dst.path", ErrInvalidInput)
	}
	srcLabel := formatEndpoint(srcName, srcPath)
	dstLabel := formatEndpoint(dstName, dstPath)
	log.Printf("storage sync start src=%s dst=%s", srcLabel, dstLabel)

	src, err := s.open(srcName)
	if err != nil {
		return IncrementalResult{}, err
	}
	defer src.Close()
	dst, err := s.open(dstName)
	if err != nil {
		return IncrementalResult{}, err
	}
	defer dst.Close()

	files, err := collectSourceFiles(ctx, src, srcPath)
	if err != nil {
		return IncrementalResult{}, err
	}
	log.Printf("storage sync listed src=%s files=%d", srcLabel, len(files))

	var result IncrementalResult
	result.Scanned = len(files)
	for index, file := range files {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		rel := relativeUnder(srcPath, file.path)
		target := dstPath
		if rel != "" {
			if target == "" {
				target = rel
			} else {
				target = path.Join(dstPath, rel)
			}
		} else if target == "" {
			target = path.Base(file.path)
		}
		destEntry, destErr := dst.Stat(ctx, target)
		if destErr != nil && !isNotFound(destErr) {
			return result, fmt.Errorf("stat %s on %s: %w", target, dstName, destErr)
		}
		reason, ok := incrementalCopyReason(file, destEntry, destErr)
		if !ok {
			result.Skipped++
			continue
		}
		log.Printf(
			"storage sync copy %d/%d reason=%s src=%s:%s dst=%s:%s size=%s",
			index+1, len(files), reason, srcName, file.path, dstName, target, formatByteSize(file.size),
		)
		started := time.Now()
		err := copyOneFile(
			ctx, s.transfers, src, dst, srcName, dstName,
			file.path, target, file.size, true, nil,
		)
		if err != nil {
			if errors.Is(err, errTransferInFlight) {
				log.Printf(
					"storage sync skip in-flight src=%s:%s dst=%s:%s",
					srcName, file.path, dstName, target,
				)
				result.Skipped++
				continue
			}
			log.Printf(
				"storage sync copy failed src=%s:%s dst=%s:%s err=%v",
				srcName, file.path, dstName, target, err,
			)
			return result, fmt.Errorf("%s -> %s: %w", file.path, target, err)
		}
		log.Printf(
			"storage sync copied src=%s:%s dst=%s:%s size=%s elapsed=%s",
			srcName, file.path, dstName, target, formatByteSize(file.size),
			time.Since(started).Round(time.Millisecond),
		)
		result.Copied++
		result.Bytes += file.size
	}
	return result, nil
}

type listedFile struct {
	path    string
	size    int64
	modTime time.Time
}

func collectSourceFiles(ctx context.Context, backend Backend, sourcePath string) ([]listedFile, error) {
	if sourcePath == "" {
		return listFilesRecursive(ctx, backend, "")
	}
	entry, err := backend.Stat(ctx, sourcePath)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir {
		return []listedFile{{
			path:    sourcePath,
			size:    entry.Size,
			modTime: entry.ModTime,
		}}, nil
	}
	return listFilesRecursive(ctx, backend, sourcePath)
}

func listFilesRecursive(ctx context.Context, backend Backend, dir string) ([]listedFile, error) {
	entries, err := backend.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	files := make([]listedFile, 0)
	for _, entry := range entries {
		if entry.IsDir {
			nested, err := listFilesRecursive(ctx, backend, entry.Path)
			if err != nil {
				return nil, err
			}
			files = append(files, nested...)
			continue
		}
		files = append(files, listedFile{
			path:    entry.Path,
			size:    entry.Size,
			modTime: entry.ModTime,
		})
	}
	return files, nil
}

func relativeUnder(root, full string) string {
	root = strings.Trim(root, "/")
	full = strings.Trim(full, "/")
	if root == "" {
		return full
	}
	if full == root {
		return ""
	}
	prefix := root + "/"
	if strings.HasPrefix(full, prefix) {
		return strings.TrimPrefix(full, prefix)
	}
	return path.Base(full)
}

func needsIncrementalCopy(src listedFile, dest Entry, destErr error) bool {
	_, ok := incrementalCopyReason(src, dest, destErr)
	return ok
}

func incrementalCopyReason(src listedFile, dest Entry, destErr error) (string, bool) {
	if destErr != nil {
		if isNotFound(destErr) {
			return "missing", true
		}
		return "", false
	}
	if dest.IsDir {
		return "dest-is-dir", true
	}
	if src.size != dest.Size {
		return "size", true
	}
	if !src.modTime.IsZero() && !dest.ModTime.IsZero() && src.modTime.After(dest.ModTime) {
		return "mtime", true
	}
	return "", false
}

func formatEndpoint(name, path string) string {
	if path == "" {
		return name + ":/"
	}
	return name + ":" + path
}

func formatByteSize(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%dB", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	for _, unit := range units {
		size /= 1024
		if size < 1024 {
			return fmt.Sprintf("%.1f%s", size, unit)
		}
	}
	return fmt.Sprintf("%.1fPiB", size/1024)
}

func logIncremental(rule config.StorageSyncRule, result IncrementalResult, err error, elapsed time.Duration) {
	src := formatEndpoint(rule.Src.Name, rule.Src.Path)
	dst := formatEndpoint(rule.Dst.Name, rule.Dst.Path)
	if err != nil {
		log.Printf(
			"storage sync failed src=%s dst=%s scanned=%d copied=%d skipped=%d bytes=%s elapsed=%s err=%v",
			src, dst, result.Scanned, result.Copied, result.Skipped,
			formatByteSize(result.Bytes), elapsed.Round(time.Millisecond), err,
		)
		return
	}
	log.Printf(
		"storage sync done src=%s dst=%s scanned=%d copied=%d skipped=%d bytes=%s elapsed=%s",
		src, dst, result.Scanned, result.Copied, result.Skipped,
		formatByteSize(result.Bytes), elapsed.Round(time.Millisecond),
	)
}
