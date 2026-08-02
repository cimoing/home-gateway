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
