package bt

import (
	"fmt"
)

// resolveDestination resolves the local engine save path for a new task.
func (s *Service) resolveDestination(subdirectory string) (savePath string, err error) {
	savePath, err = s.config.ResolveTaskDir(subdirectory)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return savePath, nil
}
