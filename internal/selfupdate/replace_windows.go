//go:build windows

package selfupdate

import (
	"fmt"
	"os"
)

// replaceBinary replaces the binary at targetPath with the file at srcPath.
// On Windows, a running executable cannot be overwritten or deleted, but it CAN
// be renamed. We rename the old binary to .old first, then rename the new binary
// into place. The .old file is cleaned up best-effort (may fail if still running).
func replaceBinary(srcPath, targetPath string) error {
	oldPath := targetPath + ".old"

	// Remove any leftover .old from a previous update.
	_ = os.Remove(oldPath)

	// Rename the current binary out of the way (works even if it's running).
	if err := os.Rename(targetPath, oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rename old binary: %w", err)
	}

	// Move the new binary into place.
	if err := os.Rename(srcPath, targetPath); err != nil {
		// Try to restore the old binary if the rename failed.
		_ = os.Rename(oldPath, targetPath)
		return fmt.Errorf("rename new binary: %w", err)
	}

	// Best-effort cleanup of the old binary.
	_ = os.Remove(oldPath)

	return nil
}
