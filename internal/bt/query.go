package bt

import (
	"context"
	"strings"
	"time"

	"home-gateway/internal/model"
)

// ListTasks returns remote torrents enriched with live rates.
func (s *Service) ListTasks(
	_ context.Context,
	status string,
	search string,
) ([]model.BTTask, error) {
	engine := s.getEngine()
	if engine == nil {
		return nil, ErrUnavailable
	}
	remotes, err := engine.ListRemote()
	if err != nil {
		return nil, err
	}
	filtered := make([]model.BTTask, 0, len(remotes))
	for _, remote := range remotes {
		task := s.enrichRemote(remote)
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

// GetTask returns one remote torrent by Transmission id.
func (s *Service) GetTask(_ context.Context, id int64) (model.BTTask, error) {
	engine := s.getEngine()
	if engine == nil {
		return model.BTTask{}, ErrUnavailable
	}
	remote, err := engine.GetRemote(id)
	if err != nil {
		return model.BTTask{}, mapTaskNotFound(err)
	}
	return s.enrichRemote(remote), nil
}

// Files returns remote file selections for a torrent.
func (s *Service) Files(_ context.Context, taskID int64) ([]model.BTTaskFile, error) {
	engine := s.getEngine()
	if engine == nil {
		return nil, ErrUnavailable
	}
	remote, err := engine.GetRemote(taskID)
	if err != nil {
		return nil, mapTaskNotFound(err)
	}
	files := make([]model.BTTaskFile, 0, len(remote.Files))
	for _, file := range remote.Files {
		files = append(files, model.BTTaskFile{
			ID:             int64(file.Index + 1),
			TaskID:         remote.ID,
			FileIndex:      file.Index,
			Path:           file.Path,
			Length:         file.Length,
			Selected:       file.Selected,
			Priority:       file.Priority,
			CompletedBytes: file.CompletedBytes,
		})
	}
	return files, nil
}

func (s *Service) enrichRemote(remote RemoteTorrent) model.BTTask {
	task := model.BTTask{
		ID:             remote.ID,
		InfoHash:       remote.InfoHash,
		SourceType:     "magnet",
		Name:           remote.Name,
		SavePath:       remote.SavePath,
		DesiredState:   remote.DesiredState,
		Status:         remote.Status,
		ErrorMessage:   remote.Error,
		TotalBytes:     remote.TotalBytes,
		CompletedBytes: remote.CompletedBytes,
		UploadedBytes:  remote.UploadedBytes,
		Peers:          remote.Peers,
		Ratio:          shareRatio(remote.UploadedBytes, remote.DownloadedBytes, remote.TotalBytes),
		CreatedAt:      remote.AddedAt,
		UpdatedAt:      time.Now().UTC(),
	}
	if relative, err := s.config.RelativeTaskDir(remote.SavePath); err == nil {
		task.SaveSubdir = relative
	}
	s.mu.Lock()
	task.SeedingPaused = s.seedPaused[remote.InfoHash]
	s.mu.Unlock()

	now := time.Now()
	s.mu.Lock()
	previous, sampled := s.samples[remote.InfoHash]
	s.samples[remote.InfoHash] = rateSample{
		at: now, downloaded: remote.DownloadedBytes, uploaded: remote.UploadedBytes,
	}
	s.mu.Unlock()
	if sampled {
		seconds := now.Sub(previous.at).Seconds()
		if seconds > 0 {
			task.DownloadRate = max(0, int64(float64(remote.DownloadedBytes-previous.downloaded)/seconds))
			task.UploadRate = max(0, int64(float64(remote.UploadedBytes-previous.uploaded)/seconds))
		}
	}
	if task.DownloadRate > 0 && task.TotalBytes > task.CompletedBytes {
		eta := (task.TotalBytes - task.CompletedBytes) / task.DownloadRate
		task.ETASeconds = &eta
	}
	if task.Status == model.BTStateCompleted {
		completedAt := remote.AddedAt
		if completedAt.IsZero() {
			completedAt = time.Now().UTC()
		}
		task.CompletedAt = &completedAt
	}
	return task
}

// Peers returns connected peers for a remote torrent.
func (s *Service) Peers(_ context.Context, taskID int64) ([]PeerInfo, error) {
	engine := s.getEngine()
	if engine == nil {
		return nil, ErrUnavailable
	}
	runtime, ok := engine.TaskByID(taskID)
	if !ok {
		if _, err := engine.GetRemote(taskID); err != nil {
			return nil, mapTaskNotFound(err)
		}
		runtime, ok = engine.TaskByID(taskID)
		if !ok {
			return nil, ErrUnavailable
		}
	}
	peers := runtime.Peers()
	now := time.Now()
	prefix := runtime.InfoHash() + "\x00"
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
