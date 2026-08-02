package bt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"home-gateway/internal/model"
)

// Pause updates restart intent and pauses network data downloading.
func (s *Service) Pause(ctx context.Context, id int64) (model.BTTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, ok := s.runtimeTask(task.InfoHash)
	if !ok {
		return model.BTTask{}, ErrUnavailable
	}
	query := s.db.Rebind(`
		UPDATE bt_tasks SET desired_state = ?, status = ?, updated_at = ? WHERE id = ?
	`)
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(
		ctx, query, model.BTStatePaused, model.BTStatePaused, now, id,
	); err != nil {
		return model.BTTask{}, fmt.Errorf("persist paused BT state: %w", err)
	}
	runtime.Pause()
	return s.GetTask(ctx, id)
}

// Resume updates restart intent and allows selected files to download.
func (s *Service) Resume(ctx context.Context, id int64) (model.BTTask, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, ok := s.runtimeTask(task.InfoHash)
	if !ok {
		return model.BTTask{}, ErrUnavailable
	}
	status := model.BTStateMetadata
	if metadataReady(runtime) {
		status = model.BTStateDownloading
	}
	query := s.db.Rebind(`
		UPDATE bt_tasks SET desired_state = ?, status = ?, error_message = '',
		    updated_at = ? WHERE id = ?
	`)
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(
		ctx, query, model.BTStateDownloading, status, now, id,
	); err != nil {
		return model.BTTask{}, fmt.Errorf("persist resumed BT state: %w", err)
	}
	s.mu.Lock()
	delete(s.seedPaused, task.InfoHash)
	limit := s.config.SeedRatioLimit
	s.mu.Unlock()
	runtime.Resume()
	if limit > 0 {
		stats := runtime.Stats()
		if task.TotalBytes > 0 && stats.CompletedBytes >= task.TotalBytes {
			ratio := shareRatio(stats.UploadedBytes, stats.DownloadedBytes, task.TotalBytes)
			if ratio >= limit {
				runtime.PauseUpload()
				s.mu.Lock()
				s.seedPaused[task.InfoHash] = true
				s.mu.Unlock()
			}
		}
	}
	return s.GetTask(ctx, id)
}

func metadataReady(task EngineTask) bool {
	select {
	case <-task.MetadataReady():
		return true
	default:
		return false
	}
}

// UpdateFiles persists and applies all provided file selections.
func (s *Service) UpdateFiles(
	ctx context.Context,
	taskID int64,
	updates []FileSelection,
) ([]model.BTTaskFile, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	runtime, ok := s.runtimeTask(task.InfoHash)
	if !ok {
		return nil, ErrUnavailable
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("%w: at least one file selection is required", ErrInvalidInput)
	}

	files, err := s.Files(ctx, taskID)
	if err != nil {
		return nil, err
	}
	byIndex := make(map[int]*model.BTTaskFile, len(files))
	for index := range files {
		byIndex[files[index].FileIndex] = &files[index]
	}
	for _, update := range updates {
		file, exists := byIndex[update.Index]
		if !exists || update.Priority < 0 || update.Priority > 2 {
			return nil, fmt.Errorf("%w: invalid file selection", ErrInvalidInput)
		}
		file.Selected = update.Priority > 0
		file.Priority = update.Priority
	}
	allSelections := make([]FileSelection, 0, len(files))
	for _, file := range files {
		priority := 0
		if file.Selected {
			priority = file.Priority
		}
		allSelections = append(allSelections, FileSelection{
			Index: file.FileIndex, Priority: priority,
		})
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin file selection transaction: %w", err)
	}
	defer tx.Rollback()
	updateQuery := tx.Rebind(`
		UPDATE bt_task_files SET selected = ?, priority = ?
		WHERE task_id = ? AND file_index = ?
	`)
	for _, file := range files {
		if _, err := tx.ExecContext(
			ctx, updateQuery, file.Selected, file.Priority, taskID, file.FileIndex,
		); err != nil {
			return nil, fmt.Errorf("persist BT file selection: %w", err)
		}
	}
	if err := runtime.SetFiles(allSelections); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit BT file selection: %w", err)
	}
	return s.Files(ctx, taskID)
}

// Delete removes a task and optionally its selected torrent files.
func (s *Service) Delete(ctx context.Context, id int64, deleteData bool) error {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	files, err := s.Files(ctx, id)
	if err != nil {
		return err
	}
	if _, ok := s.runtimeTask(task.InfoHash); ok {
		if err := s.engine.Remove(task.InfoHash); err != nil {
			return err
		}
	}
	if deleteData {
		if err := s.deleteTaskFiles(task, files); err != nil {
			s.setTaskError(ctx, id, err)
			return err
		}
	}
	query := s.db.Rebind(`DELETE FROM bt_tasks WHERE id = ?`)
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete BT task: %w", err)
	}
	s.mu.Lock()
	delete(s.samples, task.InfoHash)
	delete(s.seedPaused, task.InfoHash)
	s.mu.Unlock()
	return nil
}

func (s *Service) deleteTaskFiles(task model.BTTask, files []model.BTTaskFile) error {
	root, err := filepath.EvalSymlinks(s.config.DownloadDir)
	if err != nil {
		return fmt.Errorf("resolve download root: %w", err)
	}
	for _, file := range files {
		target := filepath.Clean(filepath.Join(task.SavePath, filepath.FromSlash(file.Path)))
		if !isWithin(root, target) {
			return errors.New("refusing to delete data outside configured download root")
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("resolve torrent file directory: %w", err)
		}
		if !isWithin(root, parent) {
			return errors.New("refusing to follow torrent path outside download root")
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete torrent file: %w", err)
		}
	}
	return nil
}

func isWithin(root string, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
