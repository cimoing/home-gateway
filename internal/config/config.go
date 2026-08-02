package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"home-gateway/internal/datadir"

	"github.com/goccy/go-yaml"
)

const (
	DefaultPath            = "/config/config.yaml"
	DefaultDownloadDir     = "bt/downloads"
	DefaultStagingDir      = "bt/.staging"
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
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	StorageBackend   string        `yaml:"storage_backend" json:"storageBackend"`
	DownloadDir      string        `yaml:"download_dir" json:"downloadDir"`
	ListenPort       int           `yaml:"listen_port" json:"listenPort"`
	DownloadLimitBps int64         `yaml:"download_limit_bps" json:"downloadLimitBps"`
	UploadLimitBps   int64         `yaml:"upload_limit_bps" json:"uploadLimitBps"`
	SeedRatioLimit   float64       `yaml:"seed_ratio_limit" json:"seedRatioLimit"`
	SyncStrategy     string        `yaml:"sync_strategy" json:"syncStrategy"`
	SyncConcurrency  int           `yaml:"sync_concurrency" json:"syncConcurrency"`
	Block            BTBlockConfig `yaml:"block" json:"block"`

	// EngineDir is the local filesystem root used by the torrent engine.
	// When StorageBackend is empty it equals the resolved DownloadDir; when the
	// backend is remote it is a local staging directory.
	EngineDir string `yaml:"-" json:"-"`
	// StoragePrefix is DownloadDir cleaned as a path on the selected backend.
	StoragePrefix string `yaml:"-" json:"-"`
}

// BTBlockConfig lists peers to reject by client, peer ID, port, or IP/CIDR.
type BTBlockConfig struct {
	Clients  []string `yaml:"clients" json:"clients"`
	PeerIDs  []string `yaml:"peer_ids" json:"peerIds"`
	Ports    []int    `yaml:"ports" json:"ports"`
	Networks []string `yaml:"networks" json:"networks"`
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
	if err := normalizeBTBlock(&config.BT.Block); err != nil {
		return Config{}, err
	}
	expanded, err := expandStorage(config.Storage)
	if err != nil {
		return Config{}, err
	}
	config.Storage = expanded

	if err := resolveBTPaths(&config); err != nil {
		return Config{}, err
	}

	dnsExpanded, err := expandDNS(config.DNS)
	if err != nil {
		return Config{}, err
	}
	config.DNS = dnsExpanded
	return config, nil
}

func resolveBTPaths(config *Config) error {
	config.BT.StorageBackend = strings.TrimSpace(config.BT.StorageBackend)
	downloadDir := strings.TrimSpace(config.BT.DownloadDir)
	if downloadDir == "" {
		downloadDir = DefaultDownloadDir
	}
	config.BT.DownloadDir = downloadDir
	config.BT.StoragePrefix = ""

	if config.BT.StorageBackend == "" {
		absolute, err := datadir.Resolve(downloadDir)
		if err != nil {
			return fmt.Errorf("resolve bt.download_dir: %w", err)
		}
		config.BT.DownloadDir = absolute
		config.BT.EngineDir = absolute
		return nil
	}

	// The default download dir means "backend root" when a storage backend is selected.
	if downloadDir == DefaultDownloadDir {
		downloadDir = ""
		config.BT.DownloadDir = ""
	}
	prefix, err := cleanConfigRelativePath(downloadDir)
	if err != nil {
		return fmt.Errorf("bt.download_dir: %w", err)
	}
	config.BT.DownloadDir = prefix
	var backend *StorageBackendConfig
	for index := range config.Storage.Backends {
		if config.Storage.Backends[index].Name == config.BT.StorageBackend {
			backend = &config.Storage.Backends[index]
			break
		}
	}
	if backend == nil {
		return fmt.Errorf("bt.storage_backend %q is not defined in storage.backends", config.BT.StorageBackend)
	}
	if backend.Enabled != nil && !*backend.Enabled {
		return fmt.Errorf("bt.storage_backend %q is disabled", config.BT.StorageBackend)
	}
	config.BT.StoragePrefix = prefix
	switch backend.Type {
	case "local":
		root, _ := backend.Config["root"].(string)
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || !filepath.IsAbs(root) {
			return fmt.Errorf("bt.storage_backend %q has invalid local root", config.BT.StorageBackend)
		}
		engineDir := root
		if prefix != "" {
			engineDir = filepath.Join(root, filepath.FromSlash(prefix))
		}
		relative, err := filepath.Rel(root, engineDir)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("bt.download_dir escapes storage backend root")
		}
		config.BT.EngineDir = filepath.Clean(engineDir)
		return nil
	case "smb", "s3":
		staging, err := datadir.Resolve(DefaultStagingDir)
		if err != nil {
			return fmt.Errorf("resolve bt staging dir: %w", err)
		}
		config.BT.EngineDir = staging
		return nil
	default:
		return fmt.Errorf("bt.storage_backend type %q is unsupported", backend.Type)
	}
}

func cleanConfigRelativePath(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return "", nil
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") {
		return "", errors.New("must be a relative path when bt.storage_backend is set")
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", errors.New("must not contain '..'")
		}
	}
	cleaned := pathCleanPOSIX(raw)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("must not escape storage root")
	}
	return cleaned, nil
}

func pathCleanPOSIX(raw string) string {
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

func normalizeBTBlock(block *BTBlockConfig) error {
	clients := make([]string, 0, len(block.Clients))
	for _, client := range block.Clients {
		client = strings.TrimSpace(client)
		if client == "" {
			continue
		}
		clients = append(clients, client)
	}
	block.Clients = clients

	peerIDs := make([]string, 0, len(block.PeerIDs))
	seenPeerIDs := make(map[string]struct{}, len(block.PeerIDs))
	for _, peerID := range block.PeerIDs {
		peerID = strings.TrimSpace(peerID)
		if peerID == "" {
			continue
		}
		if _, exists := seenPeerIDs[peerID]; exists {
			continue
		}
		seenPeerIDs[peerID] = struct{}{}
		peerIDs = append(peerIDs, peerID)
	}
	block.PeerIDs = peerIDs

	ports := make([]int, 0, len(block.Ports))
	seenPorts := make(map[int]struct{}, len(block.Ports))
	for _, port := range block.Ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("bt.block.ports entry %d must be between 1 and 65535", port)
		}
		if _, exists := seenPorts[port]; exists {
			continue
		}
		seenPorts[port] = struct{}{}
		ports = append(ports, port)
	}
	block.Ports = ports

	networks := make([]string, 0, len(block.Networks))
	seenNetworks := make(map[string]struct{}, len(block.Networks))
	for _, entry := range block.Networks {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return fmt.Errorf("bt.block.networks entry %q must be a valid CIDR", entry)
			}
		} else if net.ParseIP(entry) == nil {
			return fmt.Errorf("bt.block.networks entry %q must be an IP or CIDR", entry)
		}
		if _, exists := seenNetworks[entry]; exists {
			continue
		}
		seenNetworks[entry] = struct{}{}
		networks = append(networks, entry)
	}
	block.Networks = networks
	return nil
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

func (c BTConfig) engineRoot() string {
	if strings.TrimSpace(c.EngineDir) != "" {
		return c.EngineDir
	}
	return c.DownloadDir
}

// ResolveTaskDir safely resolves a task subdirectory beneath the engine root.
func (c BTConfig) ResolveTaskDir(subdirectory string) (string, error) {
	root := c.engineRoot()
	subdirectory = strings.TrimSpace(subdirectory)
	if subdirectory == "" || subdirectory == "." {
		return root, nil
	}
	if filepath.IsAbs(subdirectory) {
		return "", errors.New("download subdirectory must be relative")
	}
	resolved := filepath.Clean(filepath.Join(root, subdirectory))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("download subdirectory escapes configured root")
	}
	return resolved, nil
}

// RelativeTaskDir returns a safe path suitable for API responses.
func (c BTConfig) RelativeTaskDir(path string) (string, error) {
	relative, err := filepath.Rel(c.engineRoot(), filepath.Clean(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("task path is outside configured root")
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}
