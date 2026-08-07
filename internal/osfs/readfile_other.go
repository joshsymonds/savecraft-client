//go:build !windows

package osfs

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReadFileShared reads the file at path. On non-Windows platforms plain
// os.ReadFile already coexists with a game process renaming or deleting the
// file mid-read, so no special open mode is needed.
func ReadFileShared(path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("readfile %s: %w", path, err)
	}
	return data, nil
}
