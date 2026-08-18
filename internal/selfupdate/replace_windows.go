//go:build windows

package selfupdate

import (
	"fmt"
	"os"
)

// replaceBinary replaces the binary at targetPath with the file at srcPath.
// On Windows, a running executable cannot be overwritten or deleted, but it CAN
// be renamed. The update is first staged as a sibling of the target (the
// download dir may sit on another volume, where a bare rename fails), then the
// old binary is renamed to .old and the staged copy renamed into place. The
// .old file is cleaned up best-effort (may fail if still running).
func replaceBinary(srcPath, targetPath string) error {
	stagedPath, stageErr := stageBesideTarget(srcPath, targetPath)
	if stageErr != nil {
		return stageErr
	}
	oldPath := targetPath + ".old"

	// Remove any leftover .old from a previous update.
	_ = os.Remove(oldPath)

	// Rename the current binary out of the way (works even if it's running).
	if err := os.Rename(targetPath, oldPath); err != nil && !os.IsNotExist(err) {
		cleanupTempFiles(stagedPath)
		return fmt.Errorf("rename old binary: %w", err)
	}

	// Move the staged binary into place.
	if err := os.Rename(stagedPath, targetPath); err != nil {
		// Try to restore the old binary if the rename failed.
		_ = os.Rename(oldPath, targetPath)
		cleanupTempFiles(stagedPath)
		return fmt.Errorf("rename new binary: %w", err)
	}

	// Best-effort cleanup of the old binary.
	_ = os.Remove(oldPath)

	return nil
}
