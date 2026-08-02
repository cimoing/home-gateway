package storage

import "testing"

func TestCleanRelativePath(t *testing.T) {
	cases := map[string]string{
		"":           "",
		".":          "",
		"a/b":        "a/b",
		"/a/b/":      "a/b",
		`a\b\c`:      "a/b/c",
		"videos/film": "videos/film",
	}
	for input, want := range cases {
		got, err := cleanRelativePath(input)
		if err != nil || got != want {
			t.Fatalf("cleanRelativePath(%q)=%q,%v want %q", input, got, err, want)
		}
	}
	for _, invalid := range []string{"..", "../x", "a/../b", "a/../../b"} {
		if _, err := cleanRelativePath(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestLocalBackendRoundTrip(t *testing.T) {
	root := t.TempDir()
	backend, err := newLocalBackend(root)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := backend.Mkdir(t.Context(), "films"); err != nil {
		t.Fatal(err)
	}
	writer, err := backend.Create(t.Context(), "films/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := backend.List(t.Context(), "films")
	if err != nil || len(entries) != 1 || entries[0].Name != "readme.txt" {
		t.Fatalf("unexpected entries %+v: %v", entries, err)
	}
}
