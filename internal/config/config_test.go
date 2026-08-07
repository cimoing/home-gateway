package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingOptionalConfigUsesDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	config, err := Load(filepath.Join(root, "missing.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if config.BT.Enable {
		t.Fatal("expected bt.enable default false")
	}
	if config.BT.Transmission.URL != DefaultTransmissionURL {
		t.Fatalf("unexpected transmission url %q", config.BT.Transmission.URL)
	}
	if len(config.Storage.Backends) != 0 || len(config.DNS.Cloudflare.Zones) != 0 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}

func TestBTEnableTransmissionConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	t.Setenv("TRANSMISSION_RPC_PASSWORD", "rpc-pass")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  enable: true
  download_dir: /var/lib/transmission/downloads
  listen_port: 51413
  transmission:
    url: http://transmission:9091/transmission/rpc
    username: tr
    password: ${TRANSMISSION_RPC_PASSWORD}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if !config.BT.Enable {
		t.Fatal("expected enable true")
	}
	if config.BT.EngineDir != "/var/lib/transmission/downloads" {
		t.Fatalf("engine dir %q", config.BT.EngineDir)
	}
	if config.BT.Transmission.URL != "http://transmission:9091/transmission/rpc" {
		t.Fatalf("url %q", config.BT.Transmission.URL)
	}
	if config.BT.Transmission.Username != "tr" || config.BT.Transmission.Password != "rpc-pass" {
		t.Fatalf("auth %#v", config.BT.Transmission)
	}
}

func TestExpandEnvRequiresPresentVariables(t *testing.T) {
	t.Setenv("HOME_GATEWAY_TEST_SECRET", "s3cret")
	value, err := ExpandEnv("prefix-${HOME_GATEWAY_TEST_SECRET}-suffix")
	if err != nil || value != "prefix-s3cret-suffix" {
		t.Fatalf("unexpected expand result %q (%v)", value, err)
	}
	if _, err := ExpandEnv("${HOME_GATEWAY_MISSING_SECRET}"); err == nil {
		t.Fatal("expected missing env var to fail")
	}
}

func TestStorageSyncRulesValidated(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	backendRoot := filepath.Join(root, "media")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
storage:
  backends:
    - name: local
      type: local
      config:
        root: "`+filepath.ToSlash(backendRoot)+`"
    - name: archive
      type: local
      config:
        root: "`+filepath.ToSlash(filepath.Join(root, "archive"))+`"
  sync:
    - interval: "0 */6 * * *"
      src:
        name: local
        path: movies
      dst:
        name: archive
        path: backup/movies
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Storage.Sync) != 1 {
		t.Fatalf("sync rules %#v", config.Storage.Sync)
	}
	rule := config.Storage.Sync[0]
	if rule.Interval != "0 */6 * * *" || rule.Src.Path != "movies" || rule.Dst.Path != "backup/movies" {
		t.Fatalf("rule %#v", rule)
	}
	if rule.Enabled == nil || !*rule.Enabled {
		t.Fatal("expected enabled default true")
	}

	badPath := filepath.Join(root, "bad.yaml")
	if err := os.WriteFile(badPath, []byte(`
storage:
  backends:
    - name: local
      type: local
      config:
        root: "`+filepath.ToSlash(backendRoot)+`"
  sync:
    - interval: "not a cron"
      src: { name: local, path: "" }
      dst: { name: local, path: other }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(badPath, true); err == nil {
		t.Fatal("expected invalid cron to fail")
	}
}

func TestLoadStorageAndDNSConfig(t *testing.T) {
	t.Setenv("CF_API_TOKEN", "cf-token")
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	downloadRoot := filepath.ToSlash(filepath.Join(root, "media"))
	if err := os.WriteFile(configPath, []byte(`
storage:
  backends:
    - name: local
      type: local
      config:
        root: "`+downloadRoot+`"
dns:
  cloudflare:
    token: ${CF_API_TOKEN}
    zones:
      - Example.COM
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Storage.Backends) != 1 || config.Storage.Backends[0].Name != "local" {
		t.Fatalf("unexpected storage: %+v", config.Storage)
	}
	if config.DNS.Cloudflare.Token != "cf-token" ||
		len(config.DNS.Cloudflare.Zones) != 1 ||
		config.DNS.Cloudflare.Zones[0] != "example.com" {
		t.Fatalf("unexpected dns: %+v", config.DNS.Cloudflare)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := Default()
	config.DNS.Cloudflare.Zones = []string{"example.com"}
	config.DNS.Cloudflare.Token = "token"
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DNS.Cloudflare.Token != "token" ||
		len(loaded.DNS.Cloudflare.Zones) != 1 ||
		loaded.DNS.Cloudflare.Zones[0] != "example.com" {
		t.Fatalf("unexpected saved config: %+v", loaded.DNS)
	}
}
