//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
)

// replaceBinary installs srcPath at targetPath: stage a copy beside the
// target (cross-device safe), then rename over it atomically.
func replaceBinary(srcPath, targetPath string) error {
	stagedPath, stageErr := stageBesideTarget(srcPath, targetPath)
	if stageErr != nil {
		return stageErr
	}
	if renameErr := os.Rename(stagedPath, targetPath); renameErr != nil {
		cleanupTempFiles(stagedPath)
		return fmt.Errorf("rename: %w", renameErr)
	}
	if chmodErr := os.Chmod(targetPath, 0o700); chmodErr != nil {
		return fmt.Errorf("chmod: %w", chmodErr)
	}
	return nil
}
