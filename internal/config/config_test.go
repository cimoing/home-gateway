package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndResolveTaskDir(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  enabled: true
  download_dir: downloads
  listen_port: 51413
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(root, "downloads")
	if config.BT.DownloadDir != expected || config.BT.ListenPort != 51413 {
		t.Fatalf("unexpected config: %+v", config)
	}
	resolved, err := config.BT.ResolveTaskDir("linux/isos")
	if err != nil || resolved != filepath.Join(expected, "linux", "isos") {
		t.Fatalf("unexpected task directory %q: %v", resolved, err)
	}
	for _, invalid := range []string{"../outside", filepath.Join(root, "absolute")} {
		if _, err := config.BT.ResolveTaskDir(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestMissingOptionalConfigUsesDefaults(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if config.BT.ListenPort != DefaultListenPort {
		t.Fatalf("unexpected listen port %d", config.BT.ListenPort)
	}
}

func TestBTStorageBackendResolvesLocalEngineDir(t *testing.T) {
	root := t.TempDir()
	backendRoot := filepath.Join(root, "media")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  storage_backend: local
  download_dir: torrents
storage:
  backends:
    - name: local
      type: local
      config:
        root: "`+filepath.ToSlash(backendRoot)+`"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(backendRoot, "torrents")
	if config.BT.EngineDir != expected {
		t.Fatalf("engine dir %q, want %q", config.BT.EngineDir, expected)
	}
	if config.BT.StoragePrefix != "torrents" || config.BT.StorageBackend != "local" {
		t.Fatalf("unexpected bt storage fields: %+v", config.BT)
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

func TestLoadStorageAndDNSConfig(t *testing.T) {
	t.Setenv("CF_API_TOKEN", "cf-token")
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	downloadRoot := filepath.ToSlash(filepath.Join(root, "media"))
	if err := os.WriteFile(configPath, []byte(`
bt:
  download_dir: downloads
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

func TestSavePersistsRateAndSeedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := Default()
	config.BT.DownloadLimitBps = 1024 * 100
	config.BT.UploadLimitBps = 1024 * 50
	config.BT.SeedRatioLimit = 2
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BT.DownloadLimitBps != 1024*100 ||
		loaded.BT.UploadLimitBps != 1024*50 ||
		loaded.BT.SeedRatioLimit != 2 {
		t.Fatalf("unexpected saved config: %+v", loaded.BT)
	}
}
