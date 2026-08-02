package storage

import (
	"context"
	"fmt"
	"path/filepath"
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
	mu       sync.RWMutex
	backends map[string]config.StorageBackendConfig
}

// NewService creates a storage service from YAML backends.
func NewService(backends []config.StorageBackendConfig) *Service {
	service := &Service{backends: make(map[string]config.StorageBackendConfig)}
	service.Replace(backends)
	return service
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

// ResolveForBT returns save path and sync metadata for a BT task destination.
func (s *Service) ResolveForBT(
	_ context.Context,
	backendName string,
	prefix string,
	stagingRoot string,
	taskKey string,
) (savePath string, syncStatus string, backendType string, err error) {
	row, err := s.getConfig(backendName)
	if err != nil {
		return "", "", "", err
	}
	if row.Enabled != nil && !*row.Enabled {
		return "", "", "", fmt.Errorf("%w: storage backend is disabled", ErrUnavailable)
	}
	cleanedPrefix, err := cleanRelativePath(prefix)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: invalid storage prefix", ErrInvalidInput)
	}
	switch strings.ToLower(row.Type) {
	case model.StorageTypeLocal:
		root := filepath.Clean(stringConfig(row.Config, "root"))
		if root == "" || !filepath.IsAbs(root) {
			return "", "", "", fmt.Errorf("%w: invalid local config", ErrInvalidInput)
		}
		savePath = root
		if cleanedPrefix != "" {
			savePath = filepath.Join(root, filepath.FromSlash(cleanedPrefix))
		}
		if !isWithinRoot(root, savePath) {
			return "", "", "", ErrInvalidInput
		}
		return savePath, model.BTSyncNone, row.Type, nil
	case model.StorageTypeSMB, model.StorageTypeS3:
		savePath = filepath.Join(
			filepath.Clean(stagingRoot),
			".storage",
			sanitizeName(backendName),
			taskKey,
		)
		return savePath, model.BTSyncPending, row.Type, nil
	default:
		return "", "", "", fmt.Errorf("%w: unsupported storage type", ErrInvalidInput)
	}
}

// OpenByName opens a backend for sync/file operations by name.
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

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_", " ", "_")
	return replacer.Replace(name)
}
