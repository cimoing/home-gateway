package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvName is the environment variable that sets the data root.
	EnvName = "DATA"
	// DefaultRoot is used when DATA is unset (current working directory).
	DefaultRoot = "."
)

// Root returns the data directory base from DATA, or "." when unset.
func Root() string {
	root := strings.TrimSpace(os.Getenv(EnvName))
	if root == "" {
		return DefaultRoot
	}
	return root
}

// Resolve joins a relative path with Root(). Absolute paths are cleaned as-is.
func Resolve(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		return filepath.Clean(absolute), nil
	}
	absolute, err := filepath.Abs(filepath.Join(Root(), path))
	if err != nil {
		return "", fmt.Errorf("resolve data path: %w", err)
	}
	return filepath.Clean(absolute), nil
}
