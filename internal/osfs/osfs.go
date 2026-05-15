// Package osfs provides a real filesystem implementation of daemon.FS.
package osfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// OSFS delegates to the os package. Zero-value is ready to use.
type OSFS struct{}

// New returns an OSFS instance.
func New() *OSFS { return &OSFS{} }

// Stat returns file info for the given path.
func (*OSFS) Stat(path string) (fs.FileInfo, error) {
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return info, nil
}

// ReadDir returns directory entries for the given path.
func (*OSFS) ReadDir(path string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", path, err)
	}
	return entries, nil
}

// ReadFile returns the contents of the file at the given path.
func (*OSFS) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("readfile %s: %w", path, err)
	}
	return data, nil
}

// EvalSymlinks resolves any symbolic links in path. It errors if the path
// does not exist (callers handle not-yet-existing save dirs by resolving the
// deepest existing ancestor).
func (*OSFS) EvalSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("eval symlinks %s: %w", path, err)
	}
	return resolved, nil
}
