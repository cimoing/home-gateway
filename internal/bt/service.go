package bt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "home-gateway/internal/config"
	"home-gateway/internal/model"
	"home-gateway/internal/storage"

	"github.com/jmoiron/sqlx"
)

// AddOptions controls the immutable storage location and initial state.
type AddOptions struct {
	Subdirectory   string `json:"subdirectory"`
	StorageBackend string `json:"storageBackend"`
	SyncStrategy   string `json:"syncStrategy"`
	Start          bool   `json:"start"`
}

// Settings is the runtime configuration exposed to the UI.
type Settings struct {
	Enabled          bool    `json:"enabled"`
	StorageBackend   string  `json:"storageBackend"`
	DownloadDir      string  `json:"downloadDir"`
	DownloadRoot     string  `json:"downloadRoot"`
	ListenPort       int     `json:"listenPort"`
	Running          bool    `json:"running"`
	DownloadLimitBps int64   `json:"downloadLimitBps"`
	UploadLimitBps   int64   `json:"uploadLimitBps"`
	SeedRatioLimit   float64 `json:"seedRatioLimit"`
	SyncStrategy     string  `json:"syncStrategy"`
	SyncConcurrency  int     `json:"syncConcurrency"`
}

// UpdateSettingsRequest contains mutable BT settings.
type UpdateSettingsRequest struct {
	DownloadLimitBps *int64   `json:"downloadLimitBps"`
	UploadLimitBps   *int64   `json:"uploadLimitBps"`
	SeedRatioLimit   *float64 `json:"seedRatioLimit"`
	SyncStrategy     *string  `json:"syncStrategy"`
	SyncConcurrency  *int     `json:"syncConcurrency"`
}

// Status is the process-wide BT dashboard snapshot.
type Status struct {
	DHTNodes        int   `json:"dhtNodes"`
	DHTGoodNodes    int   `json:"dhtGoodNodes"`
	DownloadRate    int64 `json:"downloadRate"`
	UploadRate      int64 `json:"uploadRate"`
	DownloadedBytes int64 `json:"downloadedBytes"`
	UploadedBytes   int64 `json:"uploadedBytes"`
}

type rateSample struct {
	at         time.Time
	downloaded int64
	uploaded   int64
}

// Service persists task intent and coordinates the runtime engine.
type Service struct {
	db         *sqlx.DB
	engine     Engine
	storage    *storage.Service
	config     appconfig.BTConfig
	configPath string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu           sync.Mutex
	samples      map[string]rateSample
	peerSamples  map[string]rateSample
	global       rateSample
	seedPaused   map[string]bool
	syncingFiles map[string]bool
	activeSyncs  int
}

// NewService creates a BT task service. engine may be nil when disabled.
func NewService(
	db *sqlx.DB,
	engine Engine,
	config appconfig.BTConfig,
	configPath string,
) *Service {
	return NewServiceWithStorage(db, engine, nil, config, configPath)
}

// NewServiceWithStorage creates a BT service with optional storage destinations.
func NewServiceWithStorage(
	db *sqlx.DB,
	engine Engine,
	storageService *storage.Service,
	config appconfig.BTConfig,
	configPath string,
) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		db: db, engine: engine, storage: storageService,
		config: config, configPath: configPath,
		ctx: ctx, cancel: cancel,
		samples:      make(map[string]rateSample),
		peerSamples:  make(map[string]rateSample),
		seedPaused:   make(map[string]bool),
		syncingFiles: make(map[string]bool),
	}
	if engine != nil {
		service.wg.Add(1)
		go service.watchSeedRatio()
	}
	return service
}

// SetStorage attaches storage management after construction.
func (s *Service) SetStorage(storageService *storage.Service) {
	s.storage = storageService
}

// Settings returns safe runtime configuration.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Settings{
		Enabled:          s.config.Enabled,
		StorageBackend:   s.config.StorageBackend,
		DownloadDir:      s.config.DownloadDir,
		DownloadRoot:     s.config.EngineDir,
		ListenPort:       s.config.ListenPort,
		Running:          s.engine != nil,
		DownloadLimitBps: s.config.DownloadLimitBps,
		UploadLimitBps:   s.config.UploadLimitBps,
		SeedRatioLimit:   s.config.SeedRatioLimit,
		SyncStrategy:     s.config.SyncStrategy,
		SyncConcurrency:  s.config.SyncConcurrency,
	}
}

// ApplyConfig updates mutable BT settings from a reloaded YAML file without restarting the engine.
func (s *Service) ApplyConfig(config appconfig.BTConfig) {
	s.mu.Lock()
	s.config.StorageBackend = config.StorageBackend
	s.config.DownloadDir = config.DownloadDir
	s.config.EngineDir = config.EngineDir
	s.config.StoragePrefix = config.StoragePrefix
	s.config.DownloadLimitBps = config.DownloadLimitBps
	s.config.UploadLimitBps = config.UploadLimitBps
	s.config.SeedRatioLimit = config.SeedRatioLimit
	s.config.SyncStrategy = config.SyncStrategy
	s.config.SyncConcurrency = config.SyncConcurrency
	downloadLimit := s.config.DownloadLimitBps
	uploadLimit := s.config.UploadLimitBps
	engine := s.engine
	s.mu.Unlock()
	if engine != nil {
		engine.SetRateLimits(downloadLimit, uploadLimit)
	}
}

// UpdateSettings persists mutable limits and applies them to the engine.
func (s *Service) UpdateSettings(request UpdateSettingsRequest) (Settings, error) {
	s.mu.Lock()
	if request.DownloadLimitBps != nil {
		if *request.DownloadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: downloadLimitBps must be >= 0", ErrInvalidInput)
		}
		s.config.DownloadLimitBps = *request.DownloadLimitBps
	}
	if request.UploadLimitBps != nil {
		if *request.UploadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: uploadLimitBps must be >= 0", ErrInvalidInput)
		}
		s.config.UploadLimitBps = *request.UploadLimitBps
	}
	if request.SeedRatioLimit != nil {
		if *request.SeedRatioLimit < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: seedRatioLimit must be >= 0", ErrInvalidInput)
		}
		s.config.SeedRatioLimit = *request.SeedRatioLimit
	}
	if request.SyncStrategy != nil {
		strategy := strings.TrimSpace(strings.ToLower(*request.SyncStrategy))
		if strategy != model.BTSyncStrategyComplete && strategy != model.BTSyncStrategyPerFile {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: syncStrategy must be complete or per_file", ErrInvalidInput)
		}
		s.config.SyncStrategy = strategy
	}
	if request.SyncConcurrency != nil {
		if *request.SyncConcurrency < 1 || *request.SyncConcurrency > 32 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: syncConcurrency must be between 1 and 32", ErrInvalidInput)
		}
		s.config.SyncConcurrency = *request.SyncConcurrency
	}
	btConfig := s.config
	downloadLimit := s.config.DownloadLimitBps
	uploadLimit := s.config.UploadLimitBps
	configPath := s.configPath
	engine := s.engine
	s.mu.Unlock()

	if configPath != "" {
		existing, err := appconfig.Load(configPath, false)
		if err != nil {
			return Settings{}, fmt.Errorf("load config for BT settings: %w", err)
		}
		existing.BT = btConfig
		if err := appconfig.Save(configPath, existing); err != nil {
			return Settings{}, fmt.Errorf("persist BT settings: %w", err)
		}
	}
	if engine != nil {
		engine.SetRateLimits(downloadLimit, uploadLimit)
	}
	return s.Settings(), nil
}

// Status returns DHT size and global transfer rates.
func (s *Service) Status() Status {
	if s.engine == nil {
		return Status{}
	}
	stats := s.engine.Stats()
	status := Status{
		DHTNodes:        stats.DHTNodes,
		DHTGoodNodes:    stats.DHTGoodNodes,
		DownloadedBytes: stats.DownloadedBytes,
		UploadedBytes:   stats.UploadedBytes,
	}
	now := time.Now()
	s.mu.Lock()
	previous := s.global
	s.global = rateSample{
		at: now, downloaded: stats.DownloadedBytes, uploaded: stats.UploadedBytes,
	}
	s.mu.Unlock()
	if !previous.at.IsZero() {
		seconds := now.Sub(previous.at).Seconds()
		if seconds > 0 {
			status.DownloadRate = max(0, int64(float64(stats.DownloadedBytes-previous.downloaded)/seconds))
			status.UploadRate = max(0, int64(float64(stats.UploadedBytes-previous.uploaded)/seconds))
		}
	}
	return status
}

// Restore recreates runtime tasks from persisted sources.
func (s *Service) Restore(ctx context.Context) error {
	if s.engine == nil {
		return nil
	}
	var tasks []model.BTTask
	if err := s.db.SelectContext(ctx, &tasks, taskSelect+` ORDER BY created_at`); err != nil {
		return fmt.Errorf("list BT tasks for restore: %w", err)
	}
	for _, task := range tasks {
		var runtime EngineTask
		var err error
		switch task.SourceType {
		case "magnet":
			runtime, err = s.engine.AddMagnet(task.SourceValue, task.SavePath)
		case "torrent":
			runtime, err = s.engine.AddTorrent(task.Metainfo, task.SavePath)
		default:
			err = fmt.Errorf("unsupported source type %q", task.SourceType)
		}
		if err != nil {
			s.setTaskError(ctx, task.ID, err)
			continue
		}
		runtime.Pause()
		s.watchMetadata(task.ID, runtime)
		if task.StorageBackend != "" && task.SyncStatus != model.BTSyncNone {
			if task.SyncStrategy == model.BTSyncStrategyPerFile {
				s.maybeEnqueuePerFileSyncs(ctx, task)
			} else if task.Status == model.BTStateCompleted && task.SyncStatus == model.BTSyncPending {
				s.enqueueSync(task.ID)
			}
		}
	}
	return nil
}

// AddMagnet persists a magnet task and starts metadata retrieval.
func (s *Service) AddMagnet(
	ctx context.Context,
	uri string,
	options AddOptions,
) (model.BTTask, error) {
	if s.engine == nil {
		return model.BTTask{}, ErrUnavailable
	}
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(strings.ToLower(uri), "magnet:?") || len(uri) > 16384 {
		return model.BTTask{}, fmt.Errorf("%w: invalid magnet URI", ErrInvalidInput)
	}
	taskKey := strconv.FormatInt(time.Now().UnixNano(), 36)
	savePath, prefix, backendName, syncStatus, err := s.resolveDestination(ctx, options, taskKey)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, err := s.engine.AddMagnet(uri, savePath)
	if err != nil {
		return model.BTTask{}, err
	}
	task, err := s.insertTask(
		ctx,
		runtime.InfoHash(),
		"magnet",
		uri,
		nil,
		savePath,
		prefix,
		backendName,
		syncStatus,
		s.resolveSyncStrategy(options.SyncStrategy),
		options.Start,
	)
	if err != nil {
		_ = s.engine.Remove(runtime.InfoHash())
		return model.BTTask{}, err
	}
	runtime.Pause()
	s.watchMetadata(task.ID, runtime)
	return s.GetTask(ctx, task.ID)
}

// AddTorrent persists an uploaded metainfo task.
func (s *Service) AddTorrent(
	ctx context.Context,
	data []byte,
	options AddOptions,
) (model.BTTask, error) {
	if s.engine == nil {
		return model.BTTask{}, ErrUnavailable
	}
	if len(data) == 0 || len(data) > 10<<20 {
		return model.BTTask{}, fmt.Errorf("%w: torrent file must be 1 byte to 10 MiB", ErrInvalidInput)
	}
	taskKey := strconv.FormatInt(time.Now().UnixNano(), 36)
	savePath, prefix, backendName, syncStatus, err := s.resolveDestination(ctx, options, taskKey)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, err := s.engine.AddTorrent(data, savePath)
	if err != nil {
		return model.BTTask{}, err
	}
	task, err := s.insertTask(
		ctx,
		runtime.InfoHash(),
		"torrent",
		"",
		data,
		savePath,
		prefix,
		backendName,
		syncStatus,
		s.resolveSyncStrategy(options.SyncStrategy),
		options.Start,
	)
	if err != nil {
		_ = s.engine.Remove(runtime.InfoHash())
		return model.BTTask{}, err
	}
	runtime.Pause()
	s.watchMetadata(task.ID, runtime)
	return s.GetTask(ctx, task.ID)
}

func (s *Service) insertTask(
	ctx context.Context,
	infoHash string,
	sourceType string,
	sourceValue string,
	metainfo []byte,
	savePath string,
	storagePrefix string,
	storageBackend string,
	syncStatus string,
	syncStrategy string,
	start bool,
) (model.BTTask, error) {
	desired := model.BTStatePaused
	if start {
		desired = model.BTStateDownloading
	}
	if syncStatus == "" {
		syncStatus = model.BTSyncNone
	}
	if syncStrategy == "" {
		syncStrategy = model.BTSyncStrategyComplete
	}
	var count int
	countQuery := s.db.Rebind(`SELECT COUNT(*) FROM bt_tasks WHERE info_hash = ?`)
	if err := s.db.GetContext(ctx, &count, countQuery, infoHash); err != nil {
		return model.BTTask{}, fmt.Errorf("check BT task: %w", err)
	}
	if count > 0 {
		return model.BTTask{}, ErrConflict
	}
	now := time.Now().UTC()
	query := s.db.Rebind(`
		INSERT INTO bt_tasks
		    (info_hash, source_type, source_value, metainfo, name, save_path,
		     storage_backend_name, storage_prefix, sync_strategy, sync_status, sync_error,
		     desired_state, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(
		ctx, query, infoHash, sourceType, sourceValue, metainfo, "", savePath,
		storageBackend, storagePrefix, syncStrategy, syncStatus, "",
		desired, model.BTStateMetadata, now, now,
	); err != nil {
		return model.BTTask{}, fmt.Errorf("insert BT task: %w", err)
	}
	return s.taskByHash(ctx, infoHash)
}

func (s *Service) watchMetadata(taskID int64, runtime EngineTask) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.ctx.Done():
			return
		case <-runtime.MetadataReady():
		}
		if err := s.applyMetadata(s.ctx, taskID, runtime); err != nil {
			s.setTaskError(s.ctx, taskID, err)
		}
	}()
}

func (s *Service) applyMetadata(ctx context.Context, taskID int64, runtime EngineTask) error {
	metadata := runtime.Metadata()
	if metadata.Name == "" {
		return errors.New("torrent metadata is empty")
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metadata transaction: %w", err)
	}
	defer tx.Rollback()

	var taskMeta struct {
		DesiredState string `db:"desired_state"`
		SyncStatus   string `db:"sync_status"`
	}
	queryMeta := tx.Rebind(`SELECT desired_state, sync_status FROM bt_tasks WHERE id = ?`)
	if err := tx.GetContext(ctx, &taskMeta, queryMeta, taskID); err != nil {
		return mapTaskNotFound(err)
	}
	fileSyncStatus := model.BTSyncNone
	if taskMeta.SyncStatus != model.BTSyncNone {
		fileSyncStatus = model.BTSyncPending
	}

	var existing []model.BTTaskFile
	queryFiles := tx.Rebind(`
		SELECT id, task_id, file_index, path, length, selected, priority, sync_status, sync_error
		FROM bt_task_files WHERE task_id = ?
	`)
	if err := tx.SelectContext(ctx, &existing, queryFiles, taskID); err != nil {
		return fmt.Errorf("read persisted BT files: %w", err)
	}
	byIndex := make(map[int]model.BTTaskFile, len(existing))
	for _, file := range existing {
		byIndex[file.FileIndex] = file
	}
	selections := make([]FileSelection, 0, len(metadata.Files))
	for _, file := range metadata.Files {
		persisted, ok := byIndex[file.Index]
		if !ok {
			insert := tx.Rebind(`
				INSERT INTO bt_task_files
				    (task_id, file_index, path, length, selected, priority, sync_status, sync_error)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`)
			if _, err := tx.ExecContext(
				ctx, insert, taskID, file.Index, file.Path, file.Length, true, 1,
				fileSyncStatus, "",
			); err != nil {
				return fmt.Errorf("insert BT task file: %w", err)
			}
			selections = append(selections, FileSelection{Index: file.Index, Priority: 1})
		} else {
			priority := 0
			if persisted.Selected {
				priority = persisted.Priority
			}
			selections = append(selections, FileSelection{Index: file.Index, Priority: priority})
		}
	}
	desired := taskMeta.DesiredState
	status := desired
	update := tx.Rebind(`
		UPDATE bt_tasks SET name = ?, total_bytes = ?, status = ?,
		    error_message = '', updated_at = ? WHERE id = ?
	`)
	if _, err := tx.ExecContext(
		ctx, update, metadata.Name, metadata.TotalBytes, status, time.Now().UTC(), taskID,
	); err != nil {
		return fmt.Errorf("update BT metadata: %w", err)
	}
	if err := runtime.SetFiles(selections); err != nil {
		return err
	}
	if desired == model.BTStateDownloading {
		runtime.Resume()
	} else {
		runtime.Pause()
	}
	return tx.Commit()
}

func (s *Service) setTaskError(ctx context.Context, taskID int64, taskErr error) {
	message := taskErr.Error()
	if len(message) > 1024 {
		message = message[:1024]
	}
	query := s.db.Rebind(`
		UPDATE bt_tasks SET status = ?, error_message = ?, updated_at = ? WHERE id = ?
	`)
	_, _ = s.db.ExecContext(
		ctx, query, model.BTStateError, message, time.Now().UTC(), taskID,
	)
}

func (s *Service) watchSeedRatio() {
	defer s.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.enforceSeedRatio()
		}
	}
}

func (s *Service) enforceSeedRatio() {
	if s.engine == nil {
		return
	}
	s.mu.Lock()
	limit := s.config.SeedRatioLimit
	s.mu.Unlock()
	if limit <= 0 {
		s.clearSeedPauses()
		return
	}
	var tasks []model.BTTask
	if err := s.db.SelectContext(s.ctx, &tasks, taskSelect); err != nil {
		return
	}
	for _, task := range tasks {
		if task.DesiredState == model.BTStatePaused {
			continue
		}
		runtime, ok := s.runtimeTask(task.InfoHash)
		if !ok {
			continue
		}
		stats := runtime.Stats()
		if task.TotalBytes <= 0 || stats.CompletedBytes < task.TotalBytes {
			continue
		}
		ratio := shareRatio(stats.UploadedBytes, stats.DownloadedBytes, task.TotalBytes)
		if ratio < limit {
			s.mu.Lock()
			wasPaused := s.seedPaused[task.InfoHash]
			if wasPaused {
				delete(s.seedPaused, task.InfoHash)
			}
			s.mu.Unlock()
			if wasPaused {
				runtime.ResumeUpload()
			}
			continue
		}
		runtime.PauseUpload()
		s.mu.Lock()
		s.seedPaused[task.InfoHash] = true
		s.mu.Unlock()
	}
}

func (s *Service) clearSeedPauses() {
	s.mu.Lock()
	hashes := make([]string, 0, len(s.seedPaused))
	for hash := range s.seedPaused {
		hashes = append(hashes, hash)
	}
	s.seedPaused = make(map[string]bool)
	s.mu.Unlock()
	for _, hash := range hashes {
		runtime, ok := s.runtimeTask(hash)
		if !ok {
			continue
		}
		var desired string
		query := s.db.Rebind(`SELECT desired_state FROM bt_tasks WHERE info_hash = ?`)
		if err := s.db.GetContext(s.ctx, &desired, query, hash); err != nil {
			continue
		}
		if desired != model.BTStatePaused {
			runtime.ResumeUpload()
		}
	}
}

func shareRatio(uploaded, downloaded, totalBytes int64) float64 {
	base := downloaded
	if base <= 0 {
		base = totalBytes
	}
	if base <= 0 {
		return 0
	}
	return float64(uploaded) / float64(base)
}

// Close stops background work and the embedded engine.
func (s *Service) Close() error {
	s.cancel()
	s.wg.Wait()
	if s.engine != nil {
		return s.engine.Close()
	}
	return nil
}

const taskSelect = `
	SELECT id, info_hash, source_type, source_value, metainfo, name, save_path,
	       storage_backend_name, storage_prefix, sync_strategy, sync_status, sync_error,
	       desired_state, status, error_message, total_bytes, completed_at,
	       created_at, updated_at
	FROM bt_tasks
`

func mapTaskNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
