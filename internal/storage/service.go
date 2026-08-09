package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"home-gateway/internal/config"
	"home-gateway/internal/model"
)

// DraftBackendRequest tests a backend that is not yet in config.
type DraftBackendRequest struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
	Secret string         `json:"secret"`
}

// BackendView is a safe API representation.
type BackendView struct {
	model.StorageBackend
	Config map[string]any `json:"config"`
}

// Service manages config-backed storage backends and file operations.
type Service struct {
	mu         sync.RWMutex
	backends   map[string]config.StorageBackendConfig
	syncJobs   *SyncJobs
	scheduler  *Scheduler
	transfers  *transferLock
}

// NewService creates a storage service from YAML backends.
func NewService(backends []config.StorageBackendConfig) *Service {
	service := &Service{
		backends:  make(map[string]config.StorageBackendConfig),
		syncJobs:  NewSyncJobs(),
		transfers: newTransferLock(),
	}
	service.Replace(backends)
	return service
}

// SetScheduler attaches the cron scheduler used for storage.sync rules.
func (s *Service) SetScheduler(scheduler *Scheduler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduler = scheduler
}

func (s *Service) getScheduler() *Scheduler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scheduler
}

// ListSyncSchedules returns configured storage.sync rules.
func (s *Service) ListSyncSchedules() []ScheduleView {
	scheduler := s.getScheduler()
	if scheduler == nil {
		return []ScheduleView{}
	}
	return scheduler.List()
}

// TriggerSyncSchedule starts one configured rule immediately.
func (s *Service) TriggerSyncSchedule(id int) (ScheduleView, error) {
	scheduler := s.getScheduler()
	if scheduler == nil {
		return ScheduleView{}, fmt.Errorf("%w: sync scheduler is not running", ErrUnavailable)
	}
	return scheduler.Trigger(id)
}

// Replace atomically reloads backends from configuration.
func (s *Service) Replace(backends []config.StorageBackendConfig) {
	next := make(map[string]config.StorageBackendConfig, len(backends))
	for _, backend := range backends {
		name := strings.TrimSpace(backend.Name)
		if name == "" {
			continue
		}
		enabled := true
		if backend.Enabled != nil {
			enabled = *backend.Enabled
		}
		backend.Enabled = &enabled
		next[name] = backend
	}
	s.mu.Lock()
	s.backends = next
	s.mu.Unlock()
}

func (s *Service) ListBackends(_ context.Context) ([]BackendView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	views := make([]BackendView, 0, len(s.backends))
	for _, backend := range s.backends {
		views = append(views, s.toView(backend))
	}
	// Stable-ish order by name via simple insertion sort for small N.
	for i := 1; i < len(views); i++ {
		j := i
		for j > 0 && views[j-1].Name > views[j].Name {
			views[j-1], views[j] = views[j], views[j-1]
			j--
		}
	}
	return views, nil
}

func (s *Service) GetBackend(_ context.Context, name string) (BackendView, error) {
	backend, err := s.getConfig(name)
	if err != nil {
		return BackendView{}, err
	}
	return s.toView(backend), nil
}

func (s *Service) TestBackend(ctx context.Context, name string) error {
	backend, err := s.open(name)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Ping(ctx)
}

func (s *Service) TestDraft(ctx context.Context, request DraftBackendRequest) error {
	enabled := true
	cfg := config.StorageBackendConfig{
		Name: request.Name, Type: request.Type, Config: request.Config,
		Secret: request.Secret, Enabled: &enabled,
	}
	backend, err := OpenFromConfig(cfg)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Ping(ctx)
}

func (s *Service) ListEntries(ctx context.Context, name string, dir string) ([]Entry, error) {
	backend, err := s.open(name)
	if err != nil {
		return nil, err
	}
	defer backend.Close()
	return backend.List(ctx, dir)
}

func (s *Service) Mkdir(ctx context.Context, name string, dir string) error {
	backend, err := s.open(name)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Mkdir(ctx, dir)
}

func (s *Service) Remove(ctx context.Context, name string, target string, recursive bool) error {
	backend, err := s.open(name)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Remove(ctx, target, recursive)
}

func (s *Service) Rename(ctx context.Context, name string, from string, to string) error {
	backend, err := s.open(name)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Rename(ctx, from, to)
}

func (s *Service) OpenFile(_ context.Context, name string, filePath string) (Backend, string, error) {
	backend, err := s.open(name)
	if err != nil {
		return nil, "", err
	}
	return backend, filePath, nil
}

func (s *Service) CreateFile(_ context.Context, name string, _ string) (Backend, error) {
	return s.open(name)
}

// StartSyncJob copies files/directories between two backends asynchronously.
func (s *Service) StartSyncJob(ctx context.Context, request SyncJobRequest) (SyncJobStatus, error) {
	return s.syncJobs.Start(ctx, s, request)
}

// GetSyncJob returns progress for a previously started sync job.
func (s *Service) GetSyncJob(id string) (SyncJobStatus, error) {
	return s.syncJobs.Get(id)
}

// CancelSyncJob requests cancellation of a running sync job.
func (s *Service) CancelSyncJob(id string) (SyncJobStatus, error) {
	return s.syncJobs.Cancel(id)
}

// OpenByName opens a backend for file operations by name.
func (s *Service) OpenByName(_ context.Context, name string) (Backend, error) {
	return s.open(name)
}

func (s *Service) open(name string) (Backend, error) {
	row, err := s.getConfig(name)
	if err != nil {
		return nil, err
	}
	if row.Enabled != nil && !*row.Enabled {
		return nil, fmt.Errorf("%w: storage backend is disabled", ErrUnavailable)
	}
	return OpenFromConfig(row)
}

func (s *Service) getConfig(name string) (config.StorageBackendConfig, error) {
	name = strings.TrimSpace(name)
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.backends[name]
	if !ok {
		return config.StorageBackendConfig{}, ErrNotFound
	}
	return item, nil
}

func (s *Service) toView(backend config.StorageBackendConfig) BackendView {
	enabled := true
	if backend.Enabled != nil {
		enabled = *backend.Enabled
	}
	return BackendView{
		StorageBackend: model.StorageBackend{
			Name: backend.Name, Type: backend.Type,
			HasSecret: strings.TrimSpace(backend.Secret) != "",
			Enabled: enabled,
		},
		Config: PublicConfig(backend),
	}
}
