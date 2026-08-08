package bt

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

func usesRemotePOSIXRoot(root string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(root), "\\", "/")
	return strings.HasPrefix(normalized, "/")
}

// resolveTaskDir safely joins a subdirectory beneath the Transmission download root.
// Roots that start with "/" are treated as transmission-daemon POSIX paths.
func resolveTaskDir(root, subdirectory string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("download directory unavailable from Transmission")
	}
	subdirectory = strings.TrimSpace(subdirectory)
	if usesRemotePOSIXRoot(root) {
		root = path.Clean(strings.ReplaceAll(root, "\\", "/"))
		if subdirectory == "" || subdirectory == "." {
			return root, nil
		}
		subdirectory = strings.ReplaceAll(subdirectory, "\\", "/")
		if path.IsAbs(subdirectory) {
			return "", errors.New("download subdirectory must be relative")
		}
		resolved := path.Clean(path.Join(root, subdirectory))
		if resolved != root && !strings.HasPrefix(resolved, root+"/") {
			return "", errors.New("download subdirectory escapes configured root")
		}
		return resolved, nil
	}
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

// relativeTaskDir returns a safe path suitable for API responses.
func relativeTaskDir(root, value string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("download directory unavailable from Transmission")
	}
	if usesRemotePOSIXRoot(root) {
		root = path.Clean(strings.ReplaceAll(root, "\\", "/"))
		cleaned := path.Clean(strings.ReplaceAll(value, "\\", "/"))
		if cleaned == root {
			return "", nil
		}
		prefix := root + "/"
		if !strings.HasPrefix(cleaned, prefix) {
			return "", errors.New("task path is outside configured root")
		}
		return strings.TrimPrefix(cleaned, prefix), nil
	}
	relative, err := filepath.Rel(root, filepath.Clean(value))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("task path is outside configured root")
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}

func (s *Service) resolveDestination(subdirectory string) (string, error) {
	root, err := s.ensureDownloadRoot()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	savePath, err := resolveTaskDir(root, subdirectory)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return savePath, nil
}

func (s *Service) ensureDownloadRoot() (string, error) {
	s.mu.Lock()
	root := s.downloadRoot
	engine := s.engine
	s.mu.Unlock()
	if root != "" {
		return root, nil
	}
	if engine == nil {
		return "", errors.New("download directory unavailable from Transmission")
	}
	remote, err := engine.SessionSettings()
	if err != nil {
		return "", err
	}
	root = strings.TrimSpace(remote.DownloadDir)
	if root == "" {
		return "", errors.New("download directory unavailable from Transmission")
	}
	s.mu.Lock()
	s.downloadRoot = root
	s.mu.Unlock()
	return root, nil
}
