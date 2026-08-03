package bt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"home-gateway/internal/model"
	"home-gateway/internal/storage"
)

const (
	syncProgressMinBytes    = 256 << 10
	syncProgressMinInterval = 500 * time.Millisecond
)

// RequestSync enqueues a background sync and returns the current task snapshot.
func (s *Service) RequestSync(ctx context.Context, id int64) (model.BTTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	if task.StorageBackend == "" || task.SyncStatus == model.BTSyncNone {
		return task, nil
	}
	if s.storage == nil {
		return model.BTTask{}, ErrUnavailable
	}
	log.Printf(
		"bt sync requested task=%d backend=%q status=%s strategy=%s",
		task.ID, task.StorageBackend, task.SyncStatus, task.SyncStrategy,
	)
	s.enqueueSync(id)
	return s.GetTask(ctx, id)
}

// SyncTask uploads unfinished selected files from local staging to the remote backend.
func (s *Service) SyncTask(ctx context.Context, id int64) (model.BTTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	if task.StorageBackend == "" || task.SyncStatus == model.BTSyncNone {
		return task, nil
	}
	if s.storage == nil {
		return model.BTTask{}, ErrUnavailable
	}
	files, err := s.listSyncableFiles(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	targets := make([]model.BTTaskFile, 0, len(files))
	var totalBytes int64
	for _, file := range files {
		if !file.Selected {
			continue
		}
		if file.SyncStatus == model.BTSyncSynced {
			continue
		}
		if file.Length > 0 && file.CompletedBytes < file.Length {
			continue
		}
		targets = append(targets, file)
		totalBytes += file.Length
	}
	if len(targets) == 0 {
		log.Printf("bt sync skipped task=%d reason=no-pending-files", id)
		_ = s.refreshTaskSyncStatus(ctx, id)
		return s.GetTask(ctx, id)
	}
	log.Printf(
		"bt sync start task=%d backend=%q files=%d bytes=%d strategy=%s",
		task.ID, task.StorageBackend, len(targets), totalBytes, task.SyncStrategy,
	)
	started := time.Now()
	if err := s.setSyncState(ctx, id, model.BTSyncSyncing, ""); err != nil {
		return model.BTTask{}, err
	}
	if err := s.syncFilesConcurrently(ctx, task, targets); err != nil {
		log.Printf(
			"bt sync failed task=%d elapsed=%s err=%v",
			id, time.Since(started).Round(time.Millisecond), err,
		)
		_ = s.setSyncState(ctx, id, model.BTSyncError, err.Error())
		_ = s.refreshTaskSyncStatus(ctx, id)
		return s.GetTask(ctx, id)
	}
	_ = s.refreshTaskSyncStatus(ctx, id)
	result, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	log.Printf(
		"bt sync finished task=%d status=%s synced=%d/%d elapsed=%s",
		id, result.SyncStatus, result.SyncedBytes, result.SyncTotalBytes,
		time.Since(started).Round(time.Millisecond),
	)
	return result, nil
}

func (s *Service) enqueueSync(taskID int64) {
	if s.storage == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		_, _ = s.SyncTask(s.ctx, taskID)
	}()
}

func (s *Service) enqueueFileSync(taskID int64, fileIndex int) {
	if s.storage == nil {
		return
	}
	key := fmt.Sprintf("%d:%d", taskID, fileIndex)
	s.mu.Lock()
	if s.syncingFiles == nil {
		s.syncingFiles = make(map[string]bool)
	}
	if s.syncingFiles[key] {
		s.mu.Unlock()
		return
	}
	s.syncingFiles[key] = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.syncingFiles, key)
			s.mu.Unlock()
		}()
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		_ = s.syncOneFileByIndex(s.ctx, taskID, fileIndex)
		_ = s.refreshTaskSyncStatus(s.ctx, taskID)
	}()
}

func (s *Service) syncOneFileByIndex(ctx context.Context, taskID int64, fileIndex int) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.StorageBackend == "" {
		return nil
	}
	files, err := s.listSyncableFiles(ctx, taskID)
	if err != nil {
		return err
	}
	var target *model.BTTaskFile
	for index := range files {
		if files[index].FileIndex == fileIndex {
			target = &files[index]
			break
		}
	}
	if target == nil || !target.Selected || target.SyncStatus == model.BTSyncSynced {
		return nil
	}
	if target.Length > 0 && target.CompletedBytes < target.Length {
		return nil
	}
	_ = s.setSyncState(ctx, taskID, model.BTSyncSyncing, "")
	return s.syncFilesConcurrently(ctx, task, []model.BTTaskFile{*target})
}

func (s *Service) syncFilesConcurrently(
	ctx context.Context,
	task model.BTTask,
	files []model.BTTaskFile,
) error {
	if len(files) == 0 {
		return nil
	}
	var waitGroup sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, file := range files {
		file := file
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := s.syncSingleFile(ctx, task, file); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	return firstErr
}

func (s *Service) syncSingleFile(ctx context.Context, task model.BTTask, file model.BTTaskFile) error {
	if err := s.acquireSyncSlot(ctx); err != nil {
		return err
	}
	defer s.releaseSyncSlot()

	started := time.Now()
	log.Printf(
		"bt sync file start task=%d index=%d path=%q size=%d backend=%q",
		task.ID, file.FileIndex, file.Path, file.Length, task.StorageBackend,
	)
	_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncSyncing, "", 0)
	localPath := filepath.Join(task.SavePath, filepath.FromSlash(file.Path))
	reader, err := os.Open(localPath)
	if err != nil {
		message := err.Error()
		log.Printf(
			"bt sync file open failed task=%d index=%d path=%q err=%v",
			task.ID, file.FileIndex, file.Path, err,
		)
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, message, 0)
		return fmt.Errorf("open local file %s: %w", file.Path, err)
	}
	defer reader.Close()

	backend, err := s.storage.OpenByName(ctx, task.StorageBackend)
	if err != nil {
		log.Printf(
			"bt sync file backend failed task=%d index=%d backend=%q err=%v",
			task.ID, file.FileIndex, task.StorageBackend, err,
		)
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, err.Error(), 0)
		return err
	}
	defer backend.Close()

	remotePath := file.Path
	if prefix := strings.Trim(task.StoragePrefix, "/"); prefix != "" {
		remotePath = prefix + "/" + file.Path
	}
	writer, err := backend.Create(ctx, remotePath)
	if err != nil {
		log.Printf(
			"bt sync file create failed task=%d index=%d remote=%q err=%v",
			task.ID, file.FileIndex, remotePath, err,
		)
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, err.Error(), 0)
		return fmt.Errorf("create remote file %s: %w", remotePath, err)
	}

	progress := &progressReader{
		reader: reader,
		report: func(synced int64) {
			_ = s.updateFileSyncedBytes(ctx, task.ID, file.FileIndex, synced)
		},
	}
	_, copyErr := io.Copy(writer, progress)
	closeErr := writer.Close()
	syncedBytes := progress.n
	if copyErr != nil {
		log.Printf(
			"bt sync file copy failed task=%d index=%d path=%q synced=%d err=%v",
			task.ID, file.FileIndex, file.Path, syncedBytes, copyErr,
		)
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, copyErr.Error(), syncedBytes)
		return copyErr
	}
	if closeErr != nil {
		log.Printf(
			"bt sync file close failed task=%d index=%d path=%q synced=%d err=%v",
			task.ID, file.FileIndex, file.Path, syncedBytes, closeErr,
		)
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, closeErr.Error(), syncedBytes)
		return closeErr
	}
	if file.Length > 0 {
		syncedBytes = file.Length
	}
	if err := s.setFileSyncState(
		ctx, task.ID, file.FileIndex, model.BTSyncSynced, "", syncedBytes,
	); err != nil {
		return err
	}
	log.Printf(
		"bt sync file done task=%d index=%d path=%q bytes=%d elapsed=%s",
		task.ID, file.FileIndex, file.Path, syncedBytes,
		time.Since(started).Round(time.Millisecond),
	)
	return nil
}

type progressReader struct {
	reader   io.Reader
	report   func(int64)
	n        int64
	lastN    int64
	lastAt   time.Time
	reported bool
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 {
		p.n += int64(n)
		if p.report != nil &&
			(!p.reported ||
				p.n-p.lastN >= syncProgressMinBytes ||
				time.Since(p.lastAt) >= syncProgressMinInterval) {
			p.reported = true
			p.lastN = p.n
			p.lastAt = time.Now()
			p.report(p.n)
		}
	}
	return n, err
}

func (s *Service) acquireSyncSlot(ctx context.Context) error {
	for {
		s.mu.Lock()
		limit := s.config.SyncConcurrency
		if limit < 1 {
			limit = 1
		}
		if s.activeSyncs < limit {
			s.activeSyncs++
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Service) releaseSyncSlot() {
	s.mu.Lock()
	if s.activeSyncs > 0 {
		s.activeSyncs--
	}
	s.mu.Unlock()
}

func (s *Service) listSyncableFiles(ctx context.Context, taskID int64) ([]model.BTTaskFile, error) {
	return s.Files(ctx, taskID)
}

func (s *Service) setFileSyncState(
	ctx context.Context,
	taskID int64,
	fileIndex int,
	status string,
	message string,
	syncedBytes int64,
) error {
	if len(message) > 1024 {
		message = message[:1024]
	}
	if syncedBytes < 0 {
		syncedBytes = 0
	}
	query := s.db.Rebind(`
		UPDATE bt_task_files
		SET sync_status = ?, sync_error = ?, synced_bytes = ?
		WHERE task_id = ? AND file_index = ?
	`)
	_, err := s.db.ExecContext(ctx, query, status, message, syncedBytes, taskID, fileIndex)
	return err
}

func (s *Service) updateFileSyncedBytes(
	ctx context.Context,
	taskID int64,
	fileIndex int,
	syncedBytes int64,
) error {
	if syncedBytes < 0 {
		syncedBytes = 0
	}
	query := s.db.Rebind(`
		UPDATE bt_task_files SET synced_bytes = ?
		WHERE task_id = ? AND file_index = ? AND sync_status = ?
	`)
	_, err := s.db.ExecContext(
		ctx, query, syncedBytes, taskID, fileIndex, model.BTSyncSyncing,
	)
	return err
}

func (s *Service) refreshTaskSyncStatus(ctx context.Context, taskID int64) error {
	var files []model.BTTaskFile
	query := s.db.Rebind(`
		SELECT sync_status, selected FROM bt_task_files WHERE task_id = ?
	`)
	if err := s.db.SelectContext(ctx, &files, query, taskID); err != nil {
		return err
	}
	selected := 0
	synced := 0
	syncing := 0
	errored := 0
	pending := 0
	for _, file := range files {
		if !file.Selected {
			continue
		}
		selected++
		switch file.SyncStatus {
		case model.BTSyncSynced:
			synced++
		case model.BTSyncSyncing:
			syncing++
		case model.BTSyncError:
			errored++
		case model.BTSyncPending:
			pending++
		}
	}
	if selected == 0 {
		return s.setSyncState(ctx, taskID, model.BTSyncNone, "")
	}
	switch {
	case syncing > 0:
		return s.setSyncState(ctx, taskID, model.BTSyncSyncing, "")
	case errored > 0:
		return s.setSyncState(ctx, taskID, model.BTSyncError, "one or more files failed to sync")
	case synced == selected:
		return s.setSyncState(ctx, taskID, model.BTSyncSynced, "")
	case pending > 0 || synced > 0:
		return s.setSyncState(ctx, taskID, model.BTSyncPending, "")
	default:
		return s.setSyncState(ctx, taskID, model.BTSyncPending, "")
	}
}

func (s *Service) setSyncState(ctx context.Context, id int64, status string, message string) error {
	if len(message) > 1024 {
		message = message[:1024]
	}
	query := s.db.Rebind(`
		UPDATE bt_tasks SET sync_status = ?, sync_error = ?, updated_at = ? WHERE id = ?
	`)
	_, err := s.db.ExecContext(ctx, query, status, message, time.Now().UTC(), id)
	return err
}

func (s *Service) resolveSyncStrategy(requested string) string {
	requested = strings.TrimSpace(strings.ToLower(requested))
	switch requested {
	case model.BTSyncStrategyComplete, model.BTSyncStrategyPerFile:
		return requested
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.config.SyncStrategy {
	case model.BTSyncStrategyComplete, model.BTSyncStrategyPerFile:
		return s.config.SyncStrategy
	default:
		return model.BTSyncStrategyComplete
	}
}

func (s *Service) resolveDestination(
	ctx context.Context,
	options AddOptions,
	infoHash string,
) (savePath string, prefix string, backendName string, syncStatus string, err error) {
	backendName = strings.TrimSpace(options.StorageBackend)
	if backendName == "" {
		backendName = strings.TrimSpace(s.config.StorageBackend)
	}
	prefix, err = joinStoragePrefix(s.defaultStoragePrefix(backendName), options.Subdirectory)
	if err != nil {
		return "", "", "", "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if s.storage == nil || backendName == "" {
		savePath, err = s.config.ResolveTaskDir(options.Subdirectory)
		if err != nil {
			return "", "", "", "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return savePath, prefix, "", model.BTSyncNone, nil
	}
	stagingRoot := s.config.EngineDir
	if stagingRoot == "" {
		stagingRoot = s.config.DownloadDir
	}
	savePath, syncStatus, _, err = s.storage.ResolveForBT(
		ctx, backendName, prefix, stagingRoot, infoHash,
	)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) ||
			errors.Is(err, storage.ErrInvalidInput) ||
			errors.Is(err, storage.ErrUnavailable) {
			return "", "", "", "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return "", "", "", "", err
	}
	return savePath, prefix, backendName, syncStatus, nil
}

func (s *Service) defaultStoragePrefix(backendName string) string {
	if backendName == "" || backendName != strings.TrimSpace(s.config.StorageBackend) {
		return ""
	}
	return s.config.StoragePrefix
}

func joinStoragePrefix(parts ...string) (string, error) {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned, err := cleanStoragePrefix(part)
		if err != nil {
			return "", err
		}
		if cleaned != "" {
			segments = append(segments, cleaned)
		}
	}
	return strings.Join(segments, "/"), nil
}

func cleanStoragePrefix(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" || raw == "." {
		return "", nil
	}
	if strings.Contains(raw, "..") {
		return "", ErrInvalidInput
	}
	return raw, nil
}

func (s *Service) maybeEnqueuePerFileSyncs(ctx context.Context, task model.BTTask) {
	if task.StorageBackend == "" ||
		task.SyncStatus == model.BTSyncNone ||
		task.SyncStrategy != model.BTSyncStrategyPerFile {
		return
	}
	files, err := s.Files(ctx, task.ID)
	if err != nil {
		return
	}
	for _, file := range files {
		if !file.Selected || file.Length <= 0 {
			continue
		}
		if file.CompletedBytes < file.Length {
			continue
		}
		switch file.SyncStatus {
		case model.BTSyncSynced, model.BTSyncSyncing, model.BTSyncError:
			continue
		case model.BTSyncNone:
			_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncPending, "", 0)
		}
		s.enqueueFileSync(task.ID, file.FileIndex)
	}
}
