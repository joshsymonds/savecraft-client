package selfupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// stageBesideTarget copies srcPath to a sibling of targetPath on the target's
// own filesystem and removes srcPath. The download lives under the data dir
// while the binary may live on another mount (~/.local/bin, a Steam Deck's
// separate volume), so a direct rename can fail with EXDEV; a sibling copy
// followed by the caller's rename is both cross-device safe and atomic at
// the final step. Returns the staged path.
func stageBesideTarget(srcPath, targetPath string) (string, error) {
	stagedPath := filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".new")
	if err := copyFile(srcPath, stagedPath); err != nil {
		cleanupTempFiles(stagedPath)
		return "", err
	}
	cleanupTempFiles(srcPath)
	return stagedPath, nil
}

func copyFile(srcPath, dstPath string) error {
	src, openErr := os.Open(filepath.Clean(srcPath))
	if openErr != nil {
		return fmt.Errorf("open update: %w", openErr)
	}
	defer func() { _ = src.Close() }()

	dst, createErr := os.OpenFile(filepath.Clean(dstPath), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if createErr != nil {
		return fmt.Errorf("create staged binary: %w", createErr)
	}
	if _, copyErr := io.Copy(dst, src); copyErr != nil {
		_ = dst.Close()
		return fmt.Errorf("copy update: %w", copyErr)
	}
	if syncErr := dst.Sync(); syncErr != nil {
		_ = dst.Close()
		return fmt.Errorf("sync staged binary: %w", syncErr)
	}
	if closeErr := dst.Close(); closeErr != nil {
		return fmt.Errorf("close staged binary: %w", closeErr)
	}
	return nil
}
