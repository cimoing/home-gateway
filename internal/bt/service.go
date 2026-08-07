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

// NewService creates a BT service. engine may be nil when disabled.
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
		Enabled:          s.config.Enable,
		Engine:           "transmission",
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

// ApplyConfig updates mutable BT settings from a reloaded YAML file.
func (s *Service) ApplyConfig(config appconfig.BTConfig) {
	s.mu.Lock()
	s.config.Enable = config.Enable
	s.config.DownloadDir = config.DownloadDir
	s.config.EngineDir = config.EngineDir
	s.config.DownloadLimitBps = config.DownloadLimitBps
	s.config.UploadLimitBps = config.UploadLimitBps
	s.config.SeedRatioLimit = config.SeedRatioLimit
	s.config.Block = config.Block
	s.config.Transmission = config.Transmission
	s.config.ListenPort = config.ListenPort
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

// Status returns session transfer rates from the remote engine.
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

// Restore is a no-op: the remote engine is the source of truth.
func (s *Service) Restore(context.Context) error { return nil }

// AddMagnet adds a magnet on the remote engine and returns its live snapshot.
func (s *Service) AddMagnet(_ context.Context, uri string, options AddOptions) (model.BTTask, error) {
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
	if options.Start {
		runtime.Resume()
	} else {
		runtime.Pause()
	}
	return s.GetTask(context.Background(), runtime.ID())
}

// AddTorrent adds a .torrent on the remote engine and returns its live snapshot.
func (s *Service) AddTorrent(_ context.Context, metainfo []byte, options AddOptions) (model.BTTask, error) {
	if s.engine == nil {
		return model.BTTask{}, ErrUnavailable
	}
	if len(metainfo) == 0 {
		return model.BTTask{}, fmt.Errorf("%w: torrent metainfo is required", ErrInvalidInput)
	}
	savePath, err := s.resolveDestination(options.Subdirectory)
	if err != nil {
		return model.BTTask{}, err
	}
	runtime, err := s.engine.AddTorrent(metainfo, savePath)
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
	remotes, err := s.engine.ListRemote()
	if err != nil {
		return
	}
	for _, remote := range remotes {
		if remote.DesiredState == model.BTStatePaused {
			continue
		}
		runtime, ok := s.engine.TaskByID(remote.ID)
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
	s.mu.Lock()
	hashes := make([]string, 0, len(s.seedPaused))
	for hash := range s.seedPaused {
		hashes = append(hashes, hash)
	}
	s.seedPaused = make(map[string]bool)
	s.mu.Unlock()
	for _, hash := range hashes {
		runtime, ok := s.engine.Task(hash)
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
	if s.engine != nil {
		return s.engine.Close()
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
