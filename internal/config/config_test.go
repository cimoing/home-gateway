package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndResolveTaskDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
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
	root := t.TempDir()
	t.Setenv("DATA", root)
	config, err := Load(filepath.Join(root, "missing.yaml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if config.BT.ListenPort != DefaultListenPort {
		t.Fatalf("unexpected listen port %d", config.BT.ListenPort)
	}
	if config.BT.Engine != DefaultBTEngine {
		t.Fatalf("unexpected engine %q", config.BT.Engine)
	}
	if config.BT.Transmission.URL != DefaultTransmissionURL {
		t.Fatalf("unexpected transmission url %q", config.BT.Transmission.URL)
	}
	wantDownload := filepath.Join(root, "bt", "downloads")
	if config.BT.DownloadDir != wantDownload {
		t.Fatalf("download dir %q, want %q", config.BT.DownloadDir, wantDownload)
	}
}

func TestBTEngineTransmissionConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	t.Setenv("TRANSMISSION_RPC_PASSWORD", "rpc-pass")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  engine: transmission
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
	if config.BT.Engine != BTEngineTransmission {
		t.Fatalf("engine %q", config.BT.Engine)
	}
	if config.BT.Transmission.URL != "http://transmission:9091/transmission/rpc" {
		t.Fatalf("url %q", config.BT.Transmission.URL)
	}
	if config.BT.Transmission.Username != "tr" || config.BT.Transmission.Password != "rpc-pass" {
		t.Fatalf("auth %#v", config.BT.Transmission)
	}
}

func TestBTEngineInvalidRejected(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  engine: libtorrent
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath, true); err == nil {
		t.Fatal("expected invalid engine to fail")
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

func TestBTBlockConfigNormalized(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  download_dir: downloads
  block:
    clients:
      - Xunlei
      - ""
      - qB
    ports: [6881, 6881, 51413]
    networks:
      - 203.0.113.0/24
      - 198.51.100.10
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.BT.Block.Clients) != 2 ||
		config.BT.Block.Clients[0] != "Xunlei" ||
		config.BT.Block.Clients[1] != "qB" {
		t.Fatalf("clients: %+v", config.BT.Block.Clients)
	}
	if len(config.BT.Block.Ports) != 2 {
		t.Fatalf("ports: %+v", config.BT.Block.Ports)
	}
	if len(config.BT.Block.Networks) != 2 {
		t.Fatalf("networks: %+v", config.BT.Block.Networks)
	}
}

func TestBTBlockConfigRejectsInvalidNetwork(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bt:
  download_dir: downloads
  block:
    networks: ["not-an-ip"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath, true); err == nil {
		t.Fatal("expected invalid network error")
	}
}

func TestSavePersistsRateAndSeedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := Default()
	config.BT.DownloadLimitBps = ByteRate(1024 * 100)
	config.BT.UploadLimitBps = ByteRate(1024 * 50)
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
