package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"home-gateway/internal/datadir"

	"github.com/goccy/go-yaml"
	"github.com/robfig/cron/v3"
)

const DefaultPath = "/config/config.yaml"

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Config contains file-backed application settings.
type Config struct {
	Storage StorageConfig `yaml:"storage" json:"storage"`
	DNS     DNSConfig     `yaml:"dns" json:"dns"`
}

// StorageConfig holds named storage backends from YAML.
type StorageConfig struct {
	Backends []StorageBackendConfig `yaml:"backends" json:"backends"`
	// Sync lists scheduled incremental copy jobs between backends.
	Sync []StorageSyncRule `yaml:"sync" json:"sync"`
}

// StorageSyncRule is one cron-scheduled incremental sync from src to dst.
type StorageSyncRule struct {
	Interval string              `yaml:"interval" json:"interval"`
	Src      StorageSyncEndpoint `yaml:"src" json:"src"`
	Dst      StorageSyncEndpoint `yaml:"dst" json:"dst"`
	Enabled  *bool               `yaml:"enabled" json:"enabled"`
}

// StorageSyncEndpoint names a backend and relative directory/file path.
type StorageSyncEndpoint struct {
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
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
	return Config{}
}

// Load parses a YAML file. A missing non-required file uses defaults.
func Load(path string, required bool) (Config, error) {
	config := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return normalize(config)
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	return normalize(config)
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

func normalize(config Config) (Config, error) {
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
		if backend.Type == "local" {
			root, _ := backend.Config["root"].(string)
			root = strings.TrimSpace(root)
			if root != "" {
				resolved, err := datadir.Resolve(root)
				if err != nil {
					return StorageConfig{}, fmt.Errorf("storage.backends[%d].config.root: %w", index, err)
				}
				backend.Config["root"] = resolved
			}
		}
		if backend.Secret != "" {
			expanded, err := ExpandEnv(backend.Secret)
			if err != nil {
				return StorageConfig{}, fmt.Errorf("storage.backends[%d].secret: %w", index, err)
			}
			backend.Secret = expanded
		}
	}
	rules := make([]StorageSyncRule, 0, len(storage.Sync))
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	for index := range storage.Sync {
		rule := storage.Sync[index]
		rule.Interval = strings.TrimSpace(rule.Interval)
		rule.Src.Name = strings.TrimSpace(rule.Src.Name)
		rule.Dst.Name = strings.TrimSpace(rule.Dst.Name)
		srcPath, err := cleanSyncPath(rule.Src.Path)
		if err != nil {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].src.path: %w", index, err)
		}
		dstPath, err := cleanSyncPath(rule.Dst.Path)
		if err != nil {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].dst.path: %w", index, err)
		}
		rule.Src.Path = srcPath
		rule.Dst.Path = dstPath
		if rule.Interval == "" {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].interval is required", index)
		}
		if _, err := parser.Parse(rule.Interval); err != nil {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].interval: %w", index, err)
		}
		if rule.Src.Name == "" || rule.Dst.Name == "" {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d]: src.name and dst.name are required", index)
		}
		if _, ok := seen[rule.Src.Name]; !ok {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].src.name %q is not defined in storage.backends", index, rule.Src.Name)
		}
		if _, ok := seen[rule.Dst.Name]; !ok {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d].dst.name %q is not defined in storage.backends", index, rule.Dst.Name)
		}
		if rule.Src.Name == rule.Dst.Name && rule.Src.Path == rule.Dst.Path {
			return StorageConfig{}, fmt.Errorf("storage.sync[%d]: src and dst must differ", index)
		}
		enabled := true
		if rule.Enabled != nil {
			enabled = *rule.Enabled
		}
		rule.Enabled = &enabled
		rules = append(rules, rule)
	}
	storage.Sync = rules
	return storage, nil
}

func cleanSyncPath(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" || raw == "." {
		return "", nil
	}
	for _, part := range strings.Split(raw, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", errors.New("must not contain '..'")
		}
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(raw, "/") {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), nil
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
