package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseByteRate(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"102400", 102400},
		{"100K", 100 * 1024},
		{"100k", 100 * 1024},
		{"100KiB", 100 * 1024},
		{"10M", 10 * 1024 * 1024},
		{"1.5M", int64(1.5 * 1024 * 1024)},
		{"1G", 1024 * 1024 * 1024},
		{" 512 KB ", 512 * 1024},
	}
	for _, test := range cases {
		got, err := ParseByteRate(test.in)
		if err != nil {
			t.Fatalf("ParseByteRate(%q): %v", test.in, err)
		}
		if got != test.want {
			t.Fatalf("ParseByteRate(%q) = %d, want %d", test.in, got, test.want)
		}
	}
}

func TestParseByteRateRejectsInvalid(t *testing.T) {
	for _, in := range []string{"-1K", "10T", "abc", "10X"} {
		if _, err := ParseByteRate(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestFormatByteRate(t *testing.T) {
	cases := map[int64]string{
		0:                  "0",
		100:                "100",
		100 * 1024:         "100K",
		10 * 1024 * 1024:   "10M",
		1024 * 1024 * 1024: "1G",
	}
	for value, want := range cases {
		if got := FormatByteRate(value); got != want {
			t.Fatalf("FormatByteRate(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestByteRateYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := Default()
	config.BT.DownloadLimitBps = ByteRate(100 * 1024)
	config.BT.UploadLimitBps = ByteRate(10 * 1024 * 1024)
	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "100K") || !strings.Contains(text, "10M") {
		t.Fatalf("expected human rates in YAML, got:\n%s", text)
	}
	loaded, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BT.DownloadLimitBps != 100*1024 || loaded.BT.UploadLimitBps != 10*1024*1024 {
		t.Fatalf("unexpected rates: download=%d upload=%d", loaded.BT.DownloadLimitBps, loaded.BT.UploadLimitBps)
	}
}

func TestLoadHumanReadableRateLimits(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA", root)
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(`
bt:
  download_dir: downloads
  download_limit_bps: 512K
  upload_limit_bps: "2M"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if config.BT.DownloadLimitBps != 512*1024 {
		t.Fatalf("download limit %d", config.BT.DownloadLimitBps)
	}
	if config.BT.UploadLimitBps != 2*1024*1024 {
		t.Fatalf("upload limit %d", config.BT.UploadLimitBps)
	}
}
