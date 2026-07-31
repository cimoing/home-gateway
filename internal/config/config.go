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
	DefaultPath        = "/data/config.yaml"
	DefaultDownloadDir = "/data/downloads"
	DefaultListenPort  = 42069
)

// Config contains file-backed application settings.
type Config struct {
	BT BTConfig `yaml:"bt" json:"bt"`
}

// BTConfig controls the embedded BitTorrent client.
type BTConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	DownloadDir string `yaml:"download_dir" json:"downloadDir"`
	ListenPort  int    `yaml:"listen_port" json:"listenPort"`
}

// Default returns safe settings when the default config file is absent.
func Default() Config {
	return Config{BT: BTConfig{
		Enabled:     true,
		DownloadDir: DefaultDownloadDir,
		ListenPort:  DefaultListenPort,
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
