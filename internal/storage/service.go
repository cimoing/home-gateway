package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"home-gateway/internal/credential"
	"home-gateway/internal/model"

	"github.com/jmoiron/sqlx"
)

const defaultLocalBackendName = "Local Downloads"

// CreateBackendRequest creates or updates a storage backend.
type CreateBackendRequest struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Config  map[string]any `json:"config"`
	Secret  string         `json:"secret"`
	Enabled *bool          `json:"enabled"`
}

// BackendView is a safe API representation.
type BackendView struct {
	model.StorageBackend
	Config map[string]any `json:"config"`
}

// Service manages storage backends and file operations.
type Service struct {
	db        *sqlx.DB
	encryptor *credential.Encryptor
}

func NewService(db *sqlx.DB, encryptor *credential.Encryptor) *Service {
	return &Service{db: db, encryptor: encryptor}
}

// EnsureDefaultLocalBackend seeds a local backend from downloadDir when missing.
func (s *Service) EnsureDefaultLocalBackend(ctx context.Context, downloadDir string) error {
	downloadDir = filepath.Clean(strings.TrimSpace(downloadDir))
	if downloadDir == "" {
		return nil
	}
	var count int
	query := s.db.Rebind(`SELECT COUNT(*) FROM storage_backends WHERE name = ?`)
	if err := s.db.GetContext(ctx, &count, query, defaultLocalBackendName); err != nil {
		return fmt.Errorf("check default storage backend: %w", err)
	}
	if count > 0 {
		return nil
	}
	enabled := true
	_, err := s.CreateBackend(ctx, CreateBackendRequest{
		Name:    defaultLocalBackendName,
		Type:    model.StorageTypeLocal,
		Config:  map[string]any{"root": downloadDir},
		Enabled: &enabled,
	})
	return err
}

func (s *Service) ListBackends(ctx context.Context) ([]BackendView, error) {
	var items []model.StorageBackend
	if err := s.db.SelectContext(ctx, &items, `
		SELECT id, name, type, config_json, secret_ciphertext, secret_nonce,
		       secret_fingerprint, secret_hint, enabled, created_at, updated_at
		FROM storage_backends ORDER BY name
	`); err != nil {
		return nil, fmt.Errorf("list storage backends: %w", err)
	}
	views := make([]BackendView, 0, len(items))
	for _, item := range items {
		view, err := s.toView(item)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) GetBackend(ctx context.Context, id int64) (BackendView, error) {
	item, err := s.getBackendRow(ctx, id)
	if err != nil {
		return BackendView{}, err
	}
	return s.toView(item)
}

func (s *Service) CreateBackend(ctx context.Context, request CreateBackendRequest) (BackendView, error) {
	name, backendType, configJSON, secret, enabled, err := s.normalizeRequest(request, true)
	if err != nil {
		return BackendView{}, err
	}
	var ciphertext, nonce []byte
	var fingerprint *string
	hint := ""
	if secret != "" {
		ct, n, fp, h, err := s.encryptor.EncryptFor(credential.StorageSecretAAD, secret)
		if err != nil {
			return BackendView{}, err
		}
		ciphertext, nonce, hint = ct, n, h
		fingerprint = &fp
	}
	now := time.Now().UTC()
	query := s.db.Rebind(`
		INSERT INTO storage_backends
		    (name, type, config_json, secret_ciphertext, secret_nonce,
		     secret_fingerprint, secret_hint, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(
		ctx, query, name, backendType, configJSON, nullBytes(ciphertext), nullBytes(nonce),
		fingerprint, hint, enabled, now, now,
	); err != nil {
		if isUniqueViolation(err) {
			return BackendView{}, ErrConflict
		}
		return BackendView{}, fmt.Errorf("create storage backend: %w", err)
	}
	return s.getBackendViewByName(ctx, name)
}

func (s *Service) UpdateBackend(
	ctx context.Context,
	id int64,
	request CreateBackendRequest,
) (BackendView, error) {
	existing, err := s.getBackendRow(ctx, id)
	if err != nil {
		return BackendView{}, err
	}
	if request.Type == "" {
		request.Type = existing.Type
	}
	if request.Type != existing.Type {
		return BackendView{}, fmt.Errorf("%w: storage type cannot be changed", ErrInvalidInput)
	}
	name, _, configJSON, secret, enabled, err := s.normalizeRequest(request, false)
	if err != nil {
		return BackendView{}, err
	}
	if name == "" {
		name = existing.Name
	}
	ciphertext := existing.SecretCiphertext
	nonce := existing.SecretNonce
	fingerprint := existing.SecretFingerprint
	hint := existing.SecretHint
	if strings.TrimSpace(request.Secret) != "" {
		ct, n, fp, h, err := s.encryptor.EncryptFor(credential.StorageSecretAAD, secret)
		if err != nil {
			return BackendView{}, err
		}
		ciphertext, nonce, hint = ct, n, h
		fingerprint = &fp
	}
	if request.Enabled == nil {
		enabled = existing.Enabled
	}
	query := s.db.Rebind(`
		UPDATE storage_backends SET name = ?, config_json = ?, secret_ciphertext = ?,
		    secret_nonce = ?, secret_fingerprint = ?, secret_hint = ?, enabled = ?,
		    updated_at = ? WHERE id = ?
	`)
	if _, err := s.db.ExecContext(
		ctx, query, name, configJSON, nullBytes(ciphertext), nullBytes(nonce),
		fingerprint, hint, enabled, time.Now().UTC(), id,
	); err != nil {
		if isUniqueViolation(err) {
			return BackendView{}, ErrConflict
		}
		return BackendView{}, fmt.Errorf("update storage backend: %w", err)
	}
	return s.GetBackend(ctx, id)
}

func (s *Service) DeleteBackend(ctx context.Context, id int64) error {
	query := s.db.Rebind(`DELETE FROM storage_backends WHERE id = ?`)
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete storage backend: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) TestBackend(ctx context.Context, id int64) error {
	backend, err := s.open(ctx, id)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Ping(ctx)
}

func (s *Service) TestDraft(ctx context.Context, request CreateBackendRequest) error {
	_, backendType, configJSON, secret, _, err := s.normalizeRequest(request, true)
	if err != nil {
		return err
	}
	row := model.StorageBackend{Type: backendType, ConfigJSON: configJSON}
	backend, err := OpenBackend(row, secret)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Ping(ctx)
}

func (s *Service) ListEntries(ctx context.Context, id int64, dir string) ([]Entry, error) {
	backend, err := s.open(ctx, id)
	if err != nil {
		return nil, err
	}
	defer backend.Close()
	return backend.List(ctx, dir)
}

func (s *Service) Mkdir(ctx context.Context, id int64, dir string) error {
	backend, err := s.open(ctx, id)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Mkdir(ctx, dir)
}

func (s *Service) Remove(ctx context.Context, id int64, target string, recursive bool) error {
	backend, err := s.open(ctx, id)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Remove(ctx, target, recursive)
}

func (s *Service) Rename(ctx context.Context, id int64, from string, to string) error {
	backend, err := s.open(ctx, id)
	if err != nil {
		return err
	}
	defer backend.Close()
	return backend.Rename(ctx, from, to)
}

func (s *Service) OpenFile(ctx context.Context, id int64, filePath string) (Backend, string, error) {
	backend, err := s.open(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return backend, filePath, nil
}

func (s *Service) CreateFile(ctx context.Context, id int64, filePath string) (Backend, error) {
	return s.open(ctx, id)
}

// ResolveForBT returns save path and sync metadata for a BT task destination.
func (s *Service) ResolveForBT(
	ctx context.Context,
	backendID int64,
	prefix string,
	stagingRoot string,
	taskKey string,
) (savePath string, syncStatus string, backendType string, err error) {
	row, err := s.getBackendRow(ctx, backendID)
	if err != nil {
		return "", "", "", err
	}
	if !row.Enabled {
		return "", "", "", fmt.Errorf("%w: storage backend is disabled", ErrUnavailable)
	}
	cleanedPrefix, err := cleanRelativePath(prefix)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: invalid storage prefix", ErrInvalidInput)
	}
	switch row.Type {
	case model.StorageTypeLocal:
		var cfg LocalConfig
		if err := json.Unmarshal([]byte(row.ConfigJSON), &cfg); err != nil {
			return "", "", "", fmt.Errorf("%w: invalid local config", ErrInvalidInput)
		}
		root := filepath.Clean(cfg.Root)
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
			fmt.Sprintf("%d", backendID),
			taskKey,
		)
		return savePath, model.BTSyncPending, row.Type, nil
	default:
		return "", "", "", fmt.Errorf("%w: unsupported storage type", ErrInvalidInput)
	}
}

// OpenByID opens a backend for sync/file operations by ID.
func (s *Service) OpenByID(ctx context.Context, id int64) (Backend, error) {
	return s.open(ctx, id)
}

func (s *Service) open(ctx context.Context, id int64) (Backend, error) {
	row, err := s.getBackendRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if !row.Enabled {
		return nil, fmt.Errorf("%w: storage backend is disabled", ErrUnavailable)
	}
	secret := ""
	if len(row.SecretCiphertext) > 0 {
		secret, err = s.encryptor.DecryptFor(
			credential.StorageSecretAAD, row.SecretCiphertext, row.SecretNonce,
		)
		if err != nil {
			return nil, err
		}
	}
	return OpenBackend(row, secret)
}

func (s *Service) getBackendRow(ctx context.Context, id int64) (model.StorageBackend, error) {
	var item model.StorageBackend
	query := s.db.Rebind(`
		SELECT id, name, type, config_json, secret_ciphertext, secret_nonce,
		       secret_fingerprint, secret_hint, enabled, created_at, updated_at
		FROM storage_backends WHERE id = ?
	`)
	if err := s.db.GetContext(ctx, &item, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.StorageBackend{}, ErrNotFound
		}
		return model.StorageBackend{}, err
	}
	return item, nil
}

func (s *Service) getBackendViewByName(ctx context.Context, name string) (BackendView, error) {
	var item model.StorageBackend
	query := s.db.Rebind(`
		SELECT id, name, type, config_json, secret_ciphertext, secret_nonce,
		       secret_fingerprint, secret_hint, enabled, created_at, updated_at
		FROM storage_backends WHERE name = ?
	`)
	if err := s.db.GetContext(ctx, &item, query, name); err != nil {
		return BackendView{}, err
	}
	return s.toView(item)
}

func (s *Service) toView(item model.StorageBackend) (BackendView, error) {
	item.HasSecret = len(item.SecretCiphertext) > 0
	item.SecretCiphertext = nil
	item.SecretNonce = nil
	item.SecretFingerprint = nil
	cfg, err := PublicConfig(item)
	if err != nil {
		return BackendView{}, err
	}
	return BackendView{StorageBackend: item, Config: cfg}, nil
}

func (s *Service) normalizeRequest(
	request CreateBackendRequest,
	requireSecretForRemote bool,
) (name string, backendType string, configJSON string, secret string, enabled bool, err error) {
	name = strings.TrimSpace(request.Name)
	backendType = strings.TrimSpace(strings.ToLower(request.Type))
	secret = request.Secret
	enabled = true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if name == "" || len(name) > 128 {
		return "", "", "", "", false, fmt.Errorf("%w: name must contain 1 to 128 characters", ErrInvalidInput)
	}
	switch backendType {
	case model.StorageTypeLocal:
		root, _ := request.Config["root"].(string)
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || !filepath.IsAbs(root) {
			return "", "", "", "", false, fmt.Errorf("%w: local root must be an absolute path", ErrInvalidInput)
		}
		payload, _ := json.Marshal(LocalConfig{Root: root})
		configJSON = string(payload)
		secret = ""
	case model.StorageTypeSMB:
		cfg := SMBConfigJSON{
			Host:     stringConfig(request.Config, "host"),
			Share:    stringConfig(request.Config, "share"),
			Username: stringConfig(request.Config, "username"),
			Domain:   stringConfig(request.Config, "domain"),
			Port:     intConfig(request.Config, "port", 445),
		}
		if cfg.Host == "" || cfg.Share == "" || cfg.Username == "" {
			return "", "", "", "", false, fmt.Errorf("%w: smb host, share, and username are required", ErrInvalidInput)
		}
		if requireSecretForRemote && strings.TrimSpace(secret) == "" {
			return "", "", "", "", false, fmt.Errorf("%w: smb password is required", ErrInvalidInput)
		}
		payload, _ := json.Marshal(cfg)
		configJSON = string(payload)
	case model.StorageTypeS3:
		cfg := S3ConfigJSON{
			Endpoint:       stringConfig(request.Config, "endpoint"),
			Region:         stringConfig(request.Config, "region"),
			Bucket:         stringConfig(request.Config, "bucket"),
			Prefix:         stringConfig(request.Config, "prefix"),
			AccessKeyID:    stringConfig(request.Config, "accessKeyId"),
			ForcePathStyle: boolConfig(request.Config, "forcePathStyle"),
		}
		if cfg.Bucket == "" || cfg.AccessKeyID == "" {
			return "", "", "", "", false, fmt.Errorf("%w: s3 bucket and accessKeyId are required", ErrInvalidInput)
		}
		if requireSecretForRemote && strings.TrimSpace(secret) == "" {
			return "", "", "", "", false, fmt.Errorf("%w: s3 secret is required", ErrInvalidInput)
		}
		payload, _ := json.Marshal(cfg)
		configJSON = string(payload)
	default:
		return "", "", "", "", false, fmt.Errorf("%w: type must be local, smb, or s3", ErrInvalidInput)
	}
	return name, backendType, configJSON, secret, enabled, nil
}

func stringConfig(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func intConfig(config map[string]any, key string, fallback int) int {
	if config == nil {
		return fallback
	}
	switch value := config[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return fallback
	}
}

func boolConfig(config map[string]any, key string) bool {
	if config == nil {
		return false
	}
	value, _ := config[key].(bool)
	return value
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
