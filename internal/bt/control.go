package bt

import (
	"context"
	"fmt"
	"strings"

	"home-gateway/internal/model"
)

// Pause stops the remote torrent.
func (s *Service) Pause(_ context.Context, id int64) (model.BTTask, error) {
	runtime, err := s.runtimeByID(id)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime.Pause()
	return s.GetTask(context.Background(), id)
}

// Resume starts the remote torrent.
func (s *Service) Resume(_ context.Context, id int64) (model.BTTask, error) {
	runtime, err := s.runtimeByID(id)
	if err != nil {
		return model.BTTask{}, err
	}
	s.mu.Lock()
	delete(s.seedPaused, runtime.InfoHash())
	limit := s.config.SeedRatioLimit
	s.mu.Unlock()
	runtime.Resume()
	if limit > 0 {
		stats := runtime.Stats()
		meta := runtime.Metadata()
		if meta.TotalBytes > 0 && stats.CompletedBytes >= meta.TotalBytes {
			ratio := shareRatio(stats.UploadedBytes, stats.DownloadedBytes, meta.TotalBytes)
			if ratio >= limit {
				runtime.PauseUpload()
				s.mu.Lock()
				s.seedPaused[runtime.InfoHash()] = true
				s.mu.Unlock()
			}
		}
	}
	return s.GetTask(context.Background(), id)
}

// UpdateFiles applies file selections on the remote torrent.
func (s *Service) UpdateFiles(
	_ context.Context,
	taskID int64,
	updates []FileSelection,
) ([]model.BTTaskFile, error) {
	runtime, err := s.runtimeByID(taskID)
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("%w: at least one file selection is required", ErrInvalidInput)
	}
	files, err := s.Files(context.Background(), taskID)
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
	if err := runtime.SetFiles(allSelections); err != nil {
		return nil, err
	}
	return s.Files(context.Background(), taskID)
}

// Delete removes a torrent from the remote engine.
func (s *Service) Delete(_ context.Context, id int64, deleteData bool) error {
	engine := s.getEngine()
	if engine == nil {
		return ErrUnavailable
	}
	remote, err := engine.GetRemote(id)
	if err != nil {
		return mapTaskNotFound(err)
	}
	if err := engine.RemoveByID(id, deleteData); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.samples, remote.InfoHash)
	delete(s.seedPaused, remote.InfoHash)
	prefix := remote.InfoHash + "\x00"
	for key := range s.peerSamples {
		if strings.HasPrefix(key, prefix) {
			delete(s.peerSamples, key)
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) runtimeByID(id int64) (EngineTask, error) {
	engine := s.getEngine()
	if engine == nil {
		return nil, ErrUnavailable
	}
	if runtime, ok := engine.TaskByID(id); ok {
		return runtime, nil
	}
	if _, err := engine.GetRemote(id); err != nil {
		return nil, mapTaskNotFound(err)
	}
	runtime, ok := engine.TaskByID(id)
	if !ok {
		return nil, ErrUnavailable
	}
	return runtime, nil
}
