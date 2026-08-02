package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	DefaultPath            = "/data/config.yaml"
	DefaultDownloadDir     = "/data/downloads"
	DefaultListenPort      = 42069
	DefaultSyncStrategy    = "complete"
	DefaultSyncConcurrency = 2
)

// Config contains file-backed application settings.
type Config struct {
	BT BTConfig `yaml:"bt" json:"bt"`
}

// BTConfig controls the embedded BitTorrent client.
type BTConfig struct {
	Enabled          bool    `yaml:"enabled" json:"enabled"`
	DownloadDir      string  `yaml:"download_dir" json:"downloadDir"`
	ListenPort       int     `yaml:"listen_port" json:"listenPort"`
	DownloadLimitBps int64   `yaml:"download_limit_bps" json:"downloadLimitBps"`
	UploadLimitBps   int64   `yaml:"upload_limit_bps" json:"uploadLimitBps"`
	SeedRatioLimit   float64 `yaml:"seed_ratio_limit" json:"seedRatioLimit"`
	SyncStrategy     string  `yaml:"sync_strategy" json:"syncStrategy"`
	SyncConcurrency  int     `yaml:"sync_concurrency" json:"syncConcurrency"`
}

// Default returns safe settings when the default config file is absent.
func Default() Config {
	return Config{BT: BTConfig{
		Enabled:         true,
		DownloadDir:     DefaultDownloadDir,
		ListenPort:      DefaultListenPort,
		SyncStrategy:    DefaultSyncStrategy,
		SyncConcurrency: DefaultSyncConcurrency,
	}}
}

// Load parses a YAML file. A missing non-required file uses defaults.
func Load(path string, required bool) (Config, error) {
	config := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return normalize(config, path)
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	return normalize(config, path)
}

// Save writes the current configuration as YAML.
func Save(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}

func normalize(config Config, configPath string) (Config, error) {
	if strings.TrimSpace(config.BT.DownloadDir) == "" {
		config.BT.DownloadDir = DefaultDownloadDir
	}
	if config.BT.ListenPort == 0 {
		config.BT.ListenPort = DefaultListenPort
	}
	if config.BT.ListenPort < 1 || config.BT.ListenPort > 65535 {
		return Config{}, errors.New("bt.listen_port must be between 1 and 65535")
	}
	if config.BT.DownloadLimitBps < 0 || config.BT.UploadLimitBps < 0 {
		return Config{}, errors.New("bt rate limits must be zero or positive")
	}
	if config.BT.SeedRatioLimit < 0 {
		return Config{}, errors.New("bt.seed_ratio_limit must be zero or positive")
	}
	if strings.TrimSpace(config.BT.SyncStrategy) == "" {
		config.BT.SyncStrategy = DefaultSyncStrategy
	}
	switch config.BT.SyncStrategy {
	case "complete", "per_file":
	default:
		return Config{}, errors.New("bt.sync_strategy must be complete or per_file")
	}
	if config.BT.SyncConcurrency == 0 {
		config.BT.SyncConcurrency = DefaultSyncConcurrency
	}
	if config.BT.SyncConcurrency < 1 || config.BT.SyncConcurrency > 32 {
		return Config{}, errors.New("bt.sync_concurrency must be between 1 and 32")
	}
	if !filepath.IsAbs(config.BT.DownloadDir) {
		base := filepath.Dir(configPath)
		config.BT.DownloadDir = filepath.Join(base, config.BT.DownloadDir)
	}
	absolute, err := filepath.Abs(config.BT.DownloadDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve bt.download_dir: %w", err)
	}
	config.BT.DownloadDir = filepath.Clean(absolute)
	return config, nil
}

// ResolveTaskDir safely resolves a task subdirectory beneath DownloadDir.
func (c BTConfig) ResolveTaskDir(subdirectory string) (string, error) {
	subdirectory = strings.TrimSpace(subdirectory)
	if subdirectory == "" || subdirectory == "." {
		return c.DownloadDir, nil
	}
	if filepath.IsAbs(subdirectory) {
		return "", errors.New("download subdirectory must be relative")
	}
	resolved := filepath.Clean(filepath.Join(c.DownloadDir, subdirectory))
	relative, err := filepath.Rel(c.DownloadDir, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("download subdirectory escapes configured root")
	}
	return resolved, nil
}

// RelativeTaskDir returns a safe path suitable for API responses.
func (c BTConfig) RelativeTaskDir(path string) (string, error) {
	relative, err := filepath.Rel(c.DownloadDir, filepath.Clean(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("task path is outside configured root")
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}
