package datadir

import (
	"path/filepath"
	"testing"
)

func TestResolveRelativeUsesDATA(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvName, root)
	got, err := Resolve("db/home-gateway.db")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "db", "home-gateway.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveAbsoluteIgnoresDATA(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvName, t.TempDir())
	got, err := Resolve(filepath.Join(root, "abs.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(filepath.Join(root, "abs.db")) {
		t.Fatalf("got %q", got)
	}
}
