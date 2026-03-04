//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
)

// replaceBinary atomically replaces the binary at targetPath with the file at srcPath.
// On Unix, this is a simple rename + chmod.
func replaceBinary(srcPath, targetPath string) error {
	if err := os.Rename(srcPath, targetPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	if err := os.Chmod(targetPath, 0o700); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	return nil
}
