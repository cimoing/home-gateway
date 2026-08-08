package bt

import (
	"context"
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

// AddOptions controls the save subdirectory and initial state.
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

// Service proxies BT operations to the remote Transmission engine.
// Task state is never persisted locally.
type Service struct {
	engine           Engine
	config           appconfig.BTConfig
	configPath       string
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	mu               sync.Mutex
	watchStarted     bool
	samples          map[string]rateSample
	peerSamples      map[string]rateSample
	global           rateSample
	seedPaused       map[string]bool
	downloadRoot     string
	listenPort       int
	downloadLimitBps int64
	uploadLimitBps   int64
	seedRatioLimit   float64
}

// NewService creates a BT service. engine may be nil until AttachEngine.
func NewService(
	engine Engine,
	config appconfig.BTConfig,
	configPath string,
) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		engine: engine,
		config: config, configPath: configPath,
		ctx: ctx, cancel: cancel,
		samples:     make(map[string]rateSample),
		peerSamples: make(map[string]rateSample),
		seedPaused:  make(map[string]bool),
	}
	if engine != nil {
		service.syncLimitsFromRemote(engine)
		service.watchStarted = true
		service.wg.Add(1)
		go service.watchSeedRatio()
	}
	return service
}

func (s *Service) getEngine() Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.engine
}

// AttachEngine installs a remote engine after the HTTP server is already up.
func (s *Service) AttachEngine(engine Engine) {
	if engine == nil {
		return
	}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		_ = engine.Close()
		return
	}
	previous := s.engine
	s.engine = engine
	startWatch := !s.watchStarted
	if startWatch {
		s.watchStarted = true
	}
	s.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	s.syncLimitsFromRemote(engine)
	if startWatch {
		s.wg.Add(1)
		go s.watchSeedRatio()
	}
}

func (s *Service) syncLimitsFromRemote(engine Engine) {
	if engine == nil {
		return
	}
	remote, err := engine.SessionSettings()
	if err != nil {
		log.Printf("BT session settings read failed: %v", err)
		return
	}
	s.mu.Lock()
	s.downloadLimitBps = remote.DownloadLimitBps
	s.uploadLimitBps = remote.UploadLimitBps
	s.seedRatioLimit = remote.SeedRatioLimit
	if remote.DownloadDir != "" {
		s.downloadRoot = remote.DownloadDir
	}
	if remote.ListenPort > 0 {
		s.listenPort = remote.ListenPort
	}
	s.mu.Unlock()
}

// Settings returns live Transmission session configuration when connected.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	settings := Settings{
		Enabled:          s.config.Enable,
		Engine:           "transmission",
		DownloadDir:      s.downloadRoot,
		DownloadRoot:     s.downloadRoot,
		ListenPort:       s.listenPort,
		Running:          s.engine != nil,
		DownloadLimitBps: s.downloadLimitBps,
		UploadLimitBps:   s.uploadLimitBps,
		SeedRatioLimit:   s.seedRatioLimit,
		Block: appconfig.BTBlockConfig{
			Clients:  append([]string(nil), s.config.Block.Clients...),
			PeerIDs:  append([]string(nil), s.config.Block.PeerIDs...),
			Ports:    append([]int(nil), s.config.Block.Ports...),
			Networks: append([]string(nil), s.config.Block.Networks...),
		},
	}
	engine := s.engine
	s.mu.Unlock()
	if engine == nil {
		return settings
	}
	remote, err := engine.SessionSettings()
	if err != nil {
		log.Printf("BT session settings read failed: %v", err)
		return settings
	}
	settings.DownloadDir = remote.DownloadDir
	settings.DownloadRoot = remote.DownloadDir
	settings.ListenPort = remote.ListenPort
	settings.DownloadLimitBps = remote.DownloadLimitBps
	settings.UploadLimitBps = remote.UploadLimitBps
	settings.SeedRatioLimit = remote.SeedRatioLimit
	settings.Running = true

	s.mu.Lock()
	s.downloadLimitBps = remote.DownloadLimitBps
	s.uploadLimitBps = remote.UploadLimitBps
	s.seedRatioLimit = remote.SeedRatioLimit
	if remote.DownloadDir != "" {
		s.downloadRoot = remote.DownloadDir
	}
	if remote.ListenPort > 0 {
		s.listenPort = remote.ListenPort
	}
	s.mu.Unlock()
	return settings
}

// ApplyConfig updates local enable/block/RPC settings from YAML.
// Speed limits and seed ratio stay owned by Transmission and are refreshed from remote.
func (s *Service) ApplyConfig(config appconfig.BTConfig) {
	s.mu.Lock()
	s.config.Enable = config.Enable
	s.config.Block = config.Block
	s.config.Transmission = config.Transmission
	block := blockConfigFromApp(s.config.Block)
	engine := s.engine
	s.mu.Unlock()
	if engine != nil {
		s.syncLimitsFromRemote(engine)
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

// UpdateSettings syncs mutable limits to Transmission only (no local YAML write).
func (s *Service) UpdateSettings(request UpdateSettingsRequest) (Settings, error) {
	engine := s.getEngine()
	if engine == nil {
		return Settings{}, ErrUnavailable
	}

	s.mu.Lock()
	downloadLimit := s.downloadLimitBps
	uploadLimit := s.uploadLimitBps
	seedRatio := s.seedRatioLimit
	if request.DownloadLimitBps != nil {
		if *request.DownloadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: downloadLimitBps must be >= 0", ErrInvalidInput)
		}
		downloadLimit = *request.DownloadLimitBps
	}
	if request.UploadLimitBps != nil {
		if *request.UploadLimitBps < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: uploadLimitBps must be >= 0", ErrInvalidInput)
		}
		uploadLimit = *request.UploadLimitBps
	}
	if request.SeedRatioLimit != nil {
		if *request.SeedRatioLimit < 0 {
			s.mu.Unlock()
			return Settings{}, fmt.Errorf("%w: seedRatioLimit must be >= 0", ErrInvalidInput)
		}
		seedRatio = *request.SeedRatioLimit
	}
	s.downloadLimitBps = downloadLimit
	s.uploadLimitBps = uploadLimit
	s.seedRatioLimit = seedRatio
	s.mu.Unlock()

	if err := engine.ApplySessionLimits(downloadLimit, uploadLimit, seedRatio); err != nil {
		return Settings{}, err
	}
	return s.Settings(), nil
}

// Status returns session transfer rates from the remote engine.
func (s *Service) Status() Status {
	engine := s.getEngine()
	if engine == nil {
		return Status{}
	}
	stats := engine.Stats()
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

// Restore is a no-op: the remote engine is the source of truth.
func (s *Service) Restore(context.Context) error { return nil }

// MagnetLink returns the remote magnet URI for a torrent.
func (s *Service) MagnetLink(_ context.Context, id int64) (string, error) {
	engine := s.getEngine()
	if engine == nil {
		return "", ErrUnavailable
	}
	link, err := engine.MagnetLink(id)
	if err != nil {
		return "", mapTaskNotFound(err)
	}
	return link, nil
}

// AddMagnet adds a magnet on the remote engine and returns its live snapshot.
func (s *Service) AddMagnet(_ context.Context, uri string, options AddOptions) (model.BTTask, error) {
	engine := s.getEngine()
	if engine == nil {
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
	runtime, err := engine.AddMagnet(uri, savePath)
	if err != nil {
		return model.BTTask{}, err
	}
	if options.Start {
		runtime.Resume()
	} else {
		runtime.Pause()
	}
	return s.GetTask(context.Background(), runtime.ID())
}

// AddTorrent adds a .torrent on the remote engine and returns its live snapshot.
func (s *Service) AddTorrent(_ context.Context, metainfo []byte, options AddOptions) (model.BTTask, error) {
	engine := s.getEngine()
	if engine == nil {
		return model.BTTask{}, ErrUnavailable
	}
	if len(metainfo) == 0 {
		return model.BTTask{}, fmt.Errorf("%w: torrent metainfo is required", ErrInvalidInput)
	}
	savePath, err := s.resolveDestination(options.Subdirectory)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, err := engine.AddTorrent(metainfo, savePath)
	if err != nil {
		return model.BTTask{}, err
	}
	if options.Start {
		runtime.Resume()
	} else {
		runtime.Pause()
	}
	return s.GetTask(context.Background(), runtime.ID())
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
	engine := s.getEngine()
	if engine == nil {
		return
	}
	s.mu.Lock()
	limit := s.seedRatioLimit
	s.mu.Unlock()
	if limit <= 0 {
		s.clearSeedPauses()
		return
	}
	remotes, err := engine.ListRemote()
	if err != nil {
		return
	}
	for _, remote := range remotes {
		if remote.DesiredState == model.BTStatePaused {
			continue
		}
		runtime, ok := engine.TaskByID(remote.ID)
		if !ok {
			continue
		}
		if remote.TotalBytes <= 0 || remote.CompletedBytes < remote.TotalBytes {
			continue
		}
		ratio := shareRatio(remote.UploadedBytes, remote.DownloadedBytes, remote.TotalBytes)
		if ratio < limit {
			s.mu.Lock()
			wasPaused := s.seedPaused[remote.InfoHash]
			if wasPaused {
				delete(s.seedPaused, remote.InfoHash)
			}
			s.mu.Unlock()
			if wasPaused {
				runtime.ResumeUpload()
			}
			continue
		}
		runtime.PauseUpload()
		s.mu.Lock()
		s.seedPaused[remote.InfoHash] = true
		s.mu.Unlock()
	}
}

func (s *Service) clearSeedPauses() {
	engine := s.getEngine()
	s.mu.Lock()
	hashes := make([]string, 0, len(s.seedPaused))
	for hash := range s.seedPaused {
		hashes = append(hashes, hash)
	}
	s.seedPaused = make(map[string]bool)
	s.mu.Unlock()
	if engine == nil {
		return
	}
	for _, hash := range hashes {
		runtime, ok := engine.Task(hash)
		if !ok {
			continue
		}
		runtime.ResumeUpload()
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

// Close stops background work and the engine.
func (s *Service) Close() error {
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	engine := s.engine
	s.engine = nil
	s.mu.Unlock()
	if engine != nil {
		return engine.Close()
	}
	return nil
}

func metadataReady(task EngineTask) bool {
	select {
	case <-task.MetadataReady():
		return true
	default:
		return false
	}
}

var errNotFound = ErrNotFound

func mapTaskNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}
