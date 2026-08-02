package bt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"home-gateway/internal/model"
)

// ListTasks returns persisted tasks enriched with runtime counters.
func (s *Service) ListTasks(
	ctx context.Context,
	status string,
	search string,
) ([]model.BTTask, error) {
	var tasks []model.BTTask
	if err := s.db.SelectContext(ctx, &tasks, taskSelect+` ORDER BY created_at DESC`); err != nil {
		return nil, fmt.Errorf("list BT tasks: %w", err)
	}
	filtered := make([]model.BTTask, 0, len(tasks))
	for _, task := range tasks {
		task = s.enrichTask(ctx, task)
		if status != "" && task.Status != status {
			continue
		}
		if search != "" && !strings.Contains(
			strings.ToLower(task.Name),
			strings.ToLower(search),
		) {
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered, nil
}

// GetTask returns one task with live status.
func (s *Service) GetTask(ctx context.Context, id int64) (model.BTTask, error) {
	var task model.BTTask
	query := s.db.Rebind(taskSelect + ` WHERE id = ?`)
	if err := s.db.GetContext(ctx, &task, query, id); err != nil {
		return model.BTTask{}, mapTaskNotFound(err)
	}
	return s.enrichTask(ctx, task), nil
}

func (s *Service) taskByHash(ctx context.Context, hash string) (model.BTTask, error) {
	var task model.BTTask
	query := s.db.Rebind(taskSelect + ` WHERE info_hash = ?`)
	if err := s.db.GetContext(ctx, &task, query, hash); err != nil {
		return model.BTTask{}, mapTaskNotFound(err)
	}
	return task, nil
}

// Files returns persisted selections enriched with completed bytes.
func (s *Service) Files(ctx context.Context, taskID int64) ([]model.BTTaskFile, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var files []model.BTTaskFile
	query := s.db.Rebind(`
		SELECT id, task_id, file_index, path, length, selected, priority
		FROM bt_task_files WHERE task_id = ? ORDER BY file_index
	`)
	if err := s.db.SelectContext(ctx, &files, query, taskID); err != nil {
		return nil, fmt.Errorf("list BT task files: %w", err)
	}
	if runtime, ok := s.runtimeTask(task.InfoHash); ok {
		completed := runtime.Stats().FileCompleted
		for index := range files {
			files[index].CompletedBytes = completed[files[index].FileIndex]
		}
	}
	return files, nil
}

func (s *Service) enrichTask(ctx context.Context, task model.BTTask) model.BTTask {
	if relative, err := s.config.RelativeTaskDir(task.SavePath); err == nil {
		task.SaveSubdir = relative
	}
	runtime, ok := s.runtimeTask(task.InfoHash)
	if !ok {
		return task
	}
	stats := runtime.Stats()
	task.CompletedBytes = stats.CompletedBytes
	task.UploadedBytes = stats.UploadedBytes
	task.Peers = stats.ActivePeers
	task.Ratio = shareRatio(stats.UploadedBytes, stats.DownloadedBytes, task.TotalBytes)
	s.mu.Lock()
	task.SeedingPaused = s.seedPaused[task.InfoHash]
	s.mu.Unlock()

	now := time.Now()
	s.mu.Lock()
	previous, sampled := s.samples[task.InfoHash]
	s.samples[task.InfoHash] = rateSample{
		at: now, downloaded: stats.DownloadedBytes, uploaded: stats.UploadedBytes,
	}
	s.mu.Unlock()
	if sampled {
		seconds := now.Sub(previous.at).Seconds()
		if seconds > 0 {
			task.DownloadRate = max(0, int64(float64(stats.DownloadedBytes-previous.downloaded)/seconds))
			task.UploadRate = max(0, int64(float64(stats.UploadedBytes-previous.uploaded)/seconds))
		}
	}
	if task.DownloadRate > 0 && task.TotalBytes > task.CompletedBytes {
		eta := (task.TotalBytes - task.CompletedBytes) / task.DownloadRate
		task.ETASeconds = &eta
	}
	if task.TotalBytes > 0 && task.CompletedBytes >= task.TotalBytes &&
		task.Status != model.BTStateCompleted {
		task.Status = model.BTStateCompleted
		completedAt := time.Now().UTC()
		task.CompletedAt = &completedAt
		query := s.db.Rebind(`
			UPDATE bt_tasks SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?
		`)
		_, _ = s.db.ExecContext(
			ctx, query, model.BTStateCompleted, completedAt, completedAt, task.ID,
		)
	}
	return task
}

// Peers returns connected peers for a task with sampled transfer rates.
func (s *Service) Peers(ctx context.Context, taskID int64) ([]PeerInfo, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	runtime, ok := s.runtimeTask(task.InfoHash)
	if !ok {
		return nil, ErrUnavailable
	}
	peers := runtime.Peers()
	now := time.Now()
	prefix := task.InfoHash + "\x00"
	seen := make(map[string]struct{}, len(peers))

	s.mu.Lock()
	for index := range peers {
		key := prefix + peers[index].Address + "\x00" + peers[index].PeerID
		seen[key] = struct{}{}
		previous, sampled := s.peerSamples[key]
		s.peerSamples[key] = rateSample{
			at: now, downloaded: peers[index].Downloaded, uploaded: peers[index].Uploaded,
		}
		if !sampled {
			continue
		}
		seconds := now.Sub(previous.at).Seconds()
		if seconds <= 0 {
			continue
		}
		peers[index].DownloadRate = max(
			0, int64(float64(peers[index].Downloaded-previous.downloaded)/seconds),
		)
		peers[index].UploadRate = max(
			0, int64(float64(peers[index].Uploaded-previous.uploaded)/seconds),
		)
	}
	for key := range s.peerSamples {
		if strings.HasPrefix(key, prefix) {
			if _, ok := seen[key]; !ok {
				delete(s.peerSamples, key)
			}
		}
	}
	s.mu.Unlock()
	return peers, nil
}

func (s *Service) runtimeTask(infoHash string) (EngineTask, bool) {
	if s.engine == nil {
		return nil, false
	}
	return s.engine.Task(infoHash)
}
