package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Config contains file-backed application settings.
type Config struct {
	BT      BTConfig      `yaml:"bt" json:"bt"`
	Storage StorageConfig `yaml:"storage" json:"storage"`
	DNS     DNSConfig     `yaml:"dns" json:"dns"`
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

// StorageConfig holds named storage backends from YAML.
type StorageConfig struct {
	Backends []StorageBackendConfig `yaml:"backends" json:"backends"`
}

// StorageBackendConfig is one named destination.
type StorageBackendConfig struct {
	Name    string         `yaml:"name" json:"name"`
	Type    string         `yaml:"type" json:"type"`
	Config  map[string]any `yaml:"config" json:"config"`
	Secret  string         `yaml:"secret" json:"secret,omitempty"`
	Enabled *bool          `yaml:"enabled" json:"enabled"`
}

// DNSConfig holds Cloudflare connection settings.
type DNSConfig struct {
	Cloudflare CloudflareConfig `yaml:"cloudflare" json:"cloudflare"`
}

// CloudflareConfig lists API token and managed zones.
type CloudflareConfig struct {
	Token string   `yaml:"token" json:"token,omitempty"`
	Zones []string `yaml:"zones" json:"zones"`
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

// ExpandEnv replaces ${VAR} and $VAR references. Missing variables return an error.
func ExpandEnv(value string) (string, error) {
	var missing []string
	expanded := envPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := envPattern.FindStringSubmatch(match)
		name := groups[1]
		if name == "" {
			name = groups[2]
		}
		envValue, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return envValue
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	return expanded, nil
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

	expanded, err := expandStorage(config.Storage)
	if err != nil {
		return Config{}, err
	}
	config.Storage = expanded

	dnsExpanded, err := expandDNS(config.DNS)
	if err != nil {
		return Config{}, err
	}
	config.DNS = dnsExpanded
	return config, nil
}

func expandStorage(storage StorageConfig) (StorageConfig, error) {
	seen := make(map[string]struct{}, len(storage.Backends))
	for index := range storage.Backends {
		backend := &storage.Backends[index]
		backend.Name = strings.TrimSpace(backend.Name)
		backend.Type = strings.TrimSpace(strings.ToLower(backend.Type))
		if backend.Name == "" || len(backend.Name) > 128 {
			return StorageConfig{}, fmt.Errorf("storage.backends[%d].name must contain 1 to 128 characters", index)
		}
		if _, exists := seen[backend.Name]; exists {
			return StorageConfig{}, fmt.Errorf("storage.backends: duplicate name %q", backend.Name)
		}
		seen[backend.Name] = struct{}{}
		switch backend.Type {
		case "local", "smb", "s3":
		default:
			return StorageConfig{}, fmt.Errorf("storage.backends[%d].type must be local, smb, or s3", index)
		}
		if backend.Config == nil {
			backend.Config = map[string]any{}
		}
		for key, value := range backend.Config {
			text, ok := value.(string)
			if !ok {
				continue
			}
			expanded, err := ExpandEnv(text)
			if err != nil {
				return StorageConfig{}, fmt.Errorf("storage.backends[%d].config.%s: %w", index, key, err)
			}
			backend.Config[key] = expanded
		}
		if backend.Secret != "" {
			expanded, err := ExpandEnv(backend.Secret)
			if err != nil {
				return StorageConfig{}, fmt.Errorf("storage.backends[%d].secret: %w", index, err)
			}
			backend.Secret = expanded
		}
	}
	return storage, nil
}

func expandDNS(dns DNSConfig) (DNSConfig, error) {
	token := strings.TrimSpace(dns.Cloudflare.Token)
	if token != "" {
		expanded, err := ExpandEnv(token)
		if err != nil {
			return DNSConfig{}, fmt.Errorf("dns.cloudflare.token: %w", err)
		}
		dns.Cloudflare.Token = expanded
	}
	zones := make([]string, 0, len(dns.Cloudflare.Zones))
	seen := make(map[string]struct{}, len(dns.Cloudflare.Zones))
	for _, zone := range dns.Cloudflare.Zones {
		zone = strings.TrimSpace(strings.ToLower(zone))
		if zone == "" {
			return DNSConfig{}, errors.New("dns.cloudflare.zones entries must be non-empty")
		}
		if _, exists := seen[zone]; exists {
			continue
		}
		seen[zone] = struct{}{}
		zones = append(zones, zone)
	}
	dns.Cloudflare.Zones = zones
	if len(zones) > 0 && strings.TrimSpace(dns.Cloudflare.Token) == "" {
		return DNSConfig{}, errors.New("dns.cloudflare.token is required when zones are configured")
	}
	return dns, nil
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
