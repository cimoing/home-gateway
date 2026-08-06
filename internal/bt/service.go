package bt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "home-gateway/internal/config"
	"home-gateway/internal/model"

	"github.com/jmoiron/sqlx"
)

func blockConfigFromApp(config appconfig.BTBlockConfig) BlockConfig {
	return BlockConfig{
		Clients:  append([]string(nil), config.Clients...),
		PeerIDs:  append([]string(nil), config.PeerIDs...),
		Ports:    append([]int(nil), config.Ports...),
		Networks: append([]string(nil), config.Networks...),
	}
}

// AddBlockRequest adds one blocklist entry from the peers UI.
type AddBlockRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// AddOptions controls the immutable storage location and initial state.
type AddOptions struct {
	Subdirectory string `json:"subdirectory"`
	Start        bool   `json:"start"`
}

// Settings is the runtime configuration exposed to the UI.
type Settings struct {
	Enabled          bool                    `json:"enabled"`
	Engine           string                  `json:"engine"`
	DownloadDir      string                  `json:"downloadDir"`
	DownloadRoot     string                  `json:"downloadRoot"`
	ListenPort       int                     `json:"listenPort"`
	Running          bool                    `json:"running"`
	DownloadLimitBps int64                   `json:"downloadLimitBps"`
	UploadLimitBps   int64                   `json:"uploadLimitBps"`
	SeedRatioLimit   float64                 `json:"seedRatioLimit"`
	Block            appconfig.BTBlockConfig `json:"block"`
}

// UpdateSettingsRequest contains mutable BT settings.
type UpdateSettingsRequest struct {
	DownloadLimitBps *int64   `json:"downloadLimitBps"`
	UploadLimitBps   *int64   `json:"uploadLimitBps"`
	SeedRatioLimit   *float64 `json:"seedRatioLimit"`
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
	db          *sqlx.DB
	engine      Engine
	config      appconfig.BTConfig
	configPath  string
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	samples     map[string]rateSample
	peerSamples map[string]rateSample
	global      rateSample
	seedPaused  map[string]bool
}

// NewService creates a BT task service. engine may be nil when disabled.
func NewService(
	db *sqlx.DB,
	engine Engine,
	config appconfig.BTConfig,
	configPath string,
) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		db: db, engine: engine,
		config: config, configPath: configPath,
		ctx: ctx, cancel: cancel,
		samples:     make(map[string]rateSample),
		peerSamples: make(map[string]rateSample),
		seedPaused:  make(map[string]bool),
	}
	if engine != nil {
		service.wg.Add(1)
		go service.watchSeedRatio()
	}
	return service
}

// Settings returns safe runtime configuration.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Settings{
		Enabled:          s.config.Enabled,
		Engine:           s.config.Engine,
		DownloadDir:      s.config.DownloadDir,
		DownloadRoot:     s.config.EngineDir,
		ListenPort:       s.config.ListenPort,
		Running:          s.engine != nil,
		DownloadLimitBps: s.config.DownloadLimitBps.Int64(),
		UploadLimitBps:   s.config.UploadLimitBps.Int64(),
		SeedRatioLimit:   s.config.SeedRatioLimit,
		Block: appconfig.BTBlockConfig{
			Clients:  append([]string(nil), s.config.Block.Clients...),
			PeerIDs:  append([]string(nil), s.config.Block.PeerIDs...),
			Ports:    append([]int(nil), s.config.Block.Ports...),
			Networks: append([]string(nil), s.config.Block.Networks...),
		},
	}
}

// ApplyConfig updates mutable BT settings from a reloaded YAML file without restarting the engine.
func (s *Service) ApplyConfig(config appconfig.BTConfig) {
	s.mu.Lock()
	s.config.DownloadDir = config.DownloadDir
	s.config.EngineDir = config.EngineDir
	s.config.DownloadLimitBps = config.DownloadLimitBps
	s.config.UploadLimitBps = config.UploadLimitBps
	s.config.SeedRatioLimit = config.SeedRatioLimit
	s.config.Block = config.Block
	downloadLimit := s.config.DownloadLimitBps.Int64()
	uploadLimit := s.config.UploadLimitBps.Int64()
	block := blockConfigFromApp(s.config.Block)
	engine := s.engine
	s.mu.Unlock()
	if engine != nil {
		engine.SetRateLimits(downloadLimit, uploadLimit)
		if err := engine.SetBlockConfig(block); err != nil {
			log.Printf("BT blocklist reload failed: %v", err)
		}
	}
}

// AddBlock appends one blocklist rule, persists YAML, and applies it immediately.
func (s *Service) AddBlock(request AddBlockRequest) (appconfig.BTBlockConfig, error) {
	ruleType := strings.ToLower(strings.TrimSpace(request.Type))
	value := strings.TrimSpace(request.Value)
	if value == "" {
		return appconfig.BTBlockConfig{}, fmt.Errorf("%w: block value is required", ErrInvalidInput)
	}

	s.mu.Lock()
	block := s.config.Block
	switch ruleType {
	case "ip":
		ip := value
		if host, _, err := net.SplitHostPort(value); err == nil {
			ip = host
		}
		ip = strings.Trim(ip, "[]")
		if parsed := net.ParseIP(ip); parsed == nil {
			s.mu.Unlock()
			return appconfig.BTBlockConfig{}, fmt.Errorf("%w: invalid IP %q", ErrInvalidInput, value)
		}
		block.Networks = appendUniqueString(block.Networks, ip)
	case "client":
		block.Clients = appendUniqueString(block.Clients, value)
	case "port":
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			s.mu.Unlock()
			return appconfig.BTBlockConfig{}, fmt.Errorf("%w: invalid port %q", ErrInvalidInput, value)
		}
		block.Ports = appendUniqueInt(block.Ports, port)
	case "peerid", "peer_id":
		block.PeerIDs = appendUniqueString(block.PeerIDs, value)
	default:
		s.mu.Unlock()
		return appconfig.BTBlockConfig{}, fmt.Errorf("%w: block type must be ip, client, port, or peerId", ErrInvalidInput)
	}
	s.config.Block = block
	btConfig := s.config
	configPath := s.configPath
	engine := s.engine
	s.mu.Unlock()

	if configPath != "" {
		existing, err := appconfig.Load(configPath, false)
		if err != nil {
			return appconfig.BTBlockConfig{}, fmt.Errorf("load config for BT blocklist: %w", err)
		}
		existing.BT = btConfig
		if err := appconfig.Save(configPath, existing); err != nil {
			return appconfig.BTBlockConfig{}, fmt.Errorf("persist BT blocklist: %w", err)
		}
	}
	if engine != nil {
		if err := engine.SetBlockConfig(blockConfigFromApp(block)); err != nil {
			return appconfig.BTBlockConfig{}, err
		}
	}
	return block, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// UpdateSettings persists mutable limits and applies them to the engine.
func (s *Service) UpdateSettings(request UpdateSettingsRequest) (Settings, error) {
	s.mu.Lock()
	if request.DownloadLimitBps != nil {
		if *request.DownloadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: downloadLimitBps must be >= 0", ErrInvalidInput)
		}
		s.config.DownloadLimitBps = appconfig.ByteRate(*request.DownloadLimitBps)
	}
	if request.UploadLimitBps != nil {
		if *request.UploadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: uploadLimitBps must be >= 0", ErrInvalidInput)
		}
		s.config.UploadLimitBps = appconfig.ByteRate(*request.UploadLimitBps)
	}
	if request.SeedRatioLimit != nil {
		if *request.SeedRatioLimit < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: seedRatioLimit must be >= 0", ErrInvalidInput)
		}
		s.config.SeedRatioLimit = *request.SeedRatioLimit
	}
	btConfig := s.config
	downloadLimit := s.config.DownloadLimitBps.Int64()
	uploadLimit := s.config.UploadLimitBps.Int64()
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
		if errors.Is(err, ErrConflict) {
			var ok bool
			runtime, ok = s.engine.Task(task.InfoHash)
			if !ok {
				s.setTaskError(ctx, task.ID, fmt.Errorf("restore conflict for %s but runtime task missing", task.InfoHash))
				continue
			}
			err = nil
		}
		if err != nil {
			s.setTaskError(ctx, task.ID, err)
			continue
		}
		runtime.Pause()
		s.watchMetadata(task.ID, runtime)
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
	savePath, err := s.resolveDestination(options.Subdirectory)
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
	savePath, err := s.resolveDestination(options.Subdirectory)
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
	start bool,
) (model.BTTask, error) {
	desired := model.BTStatePaused
	if start {
		desired = model.BTStateDownloading
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
		"", "", "", model.BTSyncNone, "",
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
	}
	queryMeta := tx.Rebind(`SELECT desired_state FROM bt_tasks WHERE id = ?`)
	if err := tx.GetContext(ctx, &taskMeta, queryMeta, taskID); err != nil {
		return mapTaskNotFound(err)
	}

	var existing []model.BTTaskFile
	queryFiles := tx.Rebind(`
		SELECT id, task_id, file_index, path, length, selected, priority,
		       sync_status, sync_error, synced_bytes
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
				    (task_id, file_index, path, length, selected, priority,
				     sync_status, sync_error, synced_bytes)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
			`)
			if _, err := tx.ExecContext(
				ctx, insert, taskID, file.Index, file.Path, file.Length, true, 1,
				model.BTSyncNone, "",
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
