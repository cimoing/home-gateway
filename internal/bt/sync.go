package bt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"home-gateway/internal/model"
	"home-gateway/internal/storage"
)

// SyncTask uploads unfinished selected files from local staging to the remote backend.
func (s *Service) SyncTask(ctx context.Context, id int64) (model.BTTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	if task.StorageBackendID == nil || task.SyncStatus == model.BTSyncNone {
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
	}
	if len(targets) == 0 {
		_ = s.refreshTaskSyncStatus(ctx, id)
		return s.GetTask(ctx, id)
	}
	if err := s.setSyncState(ctx, id, model.BTSyncSyncing, ""); err != nil {
		return model.BTTask{}, err
	}
	if err := s.syncFilesConcurrently(ctx, task, targets); err != nil {
		_ = s.setSyncState(ctx, id, model.BTSyncError, err.Error())
		_ = s.refreshTaskSyncStatus(ctx, id)
		return s.GetTask(ctx, id)
	}
	_ = s.refreshTaskSyncStatus(ctx, id)
	return s.GetTask(ctx, id)
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
	if task.StorageBackendID == nil {
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

	_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncSyncing, "")
	localPath := filepath.Join(task.SavePath, filepath.FromSlash(file.Path))
	reader, err := os.Open(localPath)
	if err != nil {
		message := err.Error()
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, message)
		return fmt.Errorf("open local file %s: %w", file.Path, err)
	}
	defer reader.Close()

	backend, err := s.storage.OpenByID(ctx, *task.StorageBackendID)
	if err != nil {
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, err.Error())
		return err
	}
	defer backend.Close()

	remotePath := file.Path
	if prefix := strings.Trim(task.StoragePrefix, "/"); prefix != "" {
		remotePath = prefix + "/" + file.Path
	}
	writer, err := backend.Create(ctx, remotePath)
	if err != nil {
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, err.Error())
		return fmt.Errorf("create remote file %s: %w", remotePath, err)
	}
	_, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, copyErr.Error())
		return copyErr
	}
	if closeErr != nil {
		_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncError, closeErr.Error())
		return closeErr
	}
	return s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncSynced, "")
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
) error {
	if len(message) > 1024 {
		message = message[:1024]
	}
	query := s.db.Rebind(`
		UPDATE bt_task_files SET sync_status = ?, sync_error = ?
		WHERE task_id = ? AND file_index = ?
	`)
	_, err := s.db.ExecContext(ctx, query, status, message, taskID, fileIndex)
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
) (savePath string, prefix string, backendID *int64, syncStatus string, err error) {
	prefix = strings.TrimSpace(options.Subdirectory)
	if s.storage == nil || options.StorageBackendID <= 0 {
		savePath, err = s.config.ResolveTaskDir(options.Subdirectory)
		if err != nil {
			return "", "", nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return savePath, prefix, nil, model.BTSyncNone, nil
	}
	id := options.StorageBackendID
	savePath, syncStatus, _, err = s.storage.ResolveForBT(
		ctx, id, options.Subdirectory, s.config.DownloadDir, infoHash,
	)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) ||
			errors.Is(err, storage.ErrInvalidInput) ||
			errors.Is(err, storage.ErrUnavailable) {
			return "", "", nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return "", "", nil, "", err
	}
	cleaned, _ := cleanStoragePrefix(prefix)
	return savePath, cleaned, &id, syncStatus, nil
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
	if task.StorageBackendID == nil ||
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
			_ = s.setFileSyncState(ctx, task.ID, file.FileIndex, model.BTSyncPending, "")
		}
		s.enqueueFileSync(task.ID, file.FileIndex)
	}
}
