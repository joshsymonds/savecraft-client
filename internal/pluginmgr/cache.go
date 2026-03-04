package pluginmgr

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/joshsymonds/savecraft.gg/internal/appname"
)

// Cache manages local plugin storage on disk.
type Cache struct {
	dir string
}

// NewCache creates a Cache rooted at the given directory.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// HasVersion returns true if the cached plugin matches the given version.
func (c *Cache) HasVersion(gameID, version string) bool {
	versionPath := filepath.Join(c.dir, gameID, "version.txt")
	data, err := os.ReadFile(filepath.Clean(versionPath))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == version
}

// Read returns the cached wasm, sig, and version for a game.
func (c *Cache) Read(
	gameID string,
) (wasm, sig []byte, version string, err error) {
	gameDir := filepath.Join(c.dir, gameID)

	wasm, err = os.ReadFile(filepath.Clean(
		filepath.Join(gameDir, "parser.wasm"),
	))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read cached wasm: %w", err)
	}

	sig, err = os.ReadFile(filepath.Clean(
		filepath.Join(gameDir, "parser.wasm.sig"),
	))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read cached sig: %w", err)
	}

	vData, err := os.ReadFile(filepath.Clean(
		filepath.Join(gameDir, "version.txt"),
	))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read cached version: %w", err)
	}

	return wasm, sig, strings.TrimSpace(string(vData)), nil
}

// Write stores wasm, sig, and version for a game.
func (c *Cache) Write(
	gameID, version string, wasm, sig []byte,
) error {
	gameDir := filepath.Join(c.dir, gameID)
	if err := os.MkdirAll(gameDir, 0o750); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	wasmPath := filepath.Join(gameDir, "parser.wasm")
	if err := os.WriteFile(wasmPath, wasm, 0o600); err != nil {
		return fmt.Errorf("write cached wasm: %w", err)
	}

	sigPath := filepath.Join(gameDir, "parser.wasm.sig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		return fmt.Errorf("write cached sig: %w", err)
	}

	verPath := filepath.Join(gameDir, "version.txt")
	if err := os.WriteFile(verPath, []byte(version+"\n"), 0o600); err != nil {
		return fmt.Errorf("write cached version: %w", err)
	}

	hash := sha256.Sum256(wasm)
	hashPath := filepath.Join(gameDir, "sha256.txt")
	if err := os.WriteFile(hashPath, fmt.Appendf(nil, "%x\n", hash), 0o600); err != nil {
		return fmt.Errorf("write cached sha256: %w", err)
	}

	return nil
}

// SHA256 returns the cached SHA256 hash for a game, or "" if unavailable.
func (c *Cache) SHA256(gameID string) string {
	hashPath := filepath.Join(c.dir, gameID, "sha256.txt")
	data, err := os.ReadFile(filepath.Clean(hashPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// UpdateVersion updates version.txt and sha256.txt without touching wasm/sig.
func (c *Cache) UpdateVersion(gameID, version, sha256Hash string) error {
	gameDir := filepath.Join(c.dir, gameID)

	verPath := filepath.Join(gameDir, "version.txt")
	if err := os.WriteFile(verPath, []byte(version+"\n"), 0o600); err != nil {
		return fmt.Errorf("update cached version: %w", err)
	}

	hashPath := filepath.Join(gameDir, "sha256.txt")
	if err := os.WriteFile(hashPath, []byte(sha256Hash+"\n"), 0o600); err != nil {
		return fmt.Errorf("update cached sha256: %w", err)
	}

	return nil
}

// DefaultCacheDir returns the platform-appropriate cache directory for plugins.
// The appName determines the directory name (e.g. "savecraft" or "savecraft-staging").
func DefaultCacheDir(appName string) string {
	if env := os.Getenv("SAVECRAFT_CACHE_DIR"); env != "" {
		return env
	}

	switch runtime.GOOS {
	case "darwin":
		return darwinCacheDir(appName)
	case "windows":
		return windowsCacheDir(appName)
	default:
		return linuxCacheDir(appName)
	}
}

func darwinCacheDir(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName, "plugins")
	}
	return filepath.Join(
		home, "Library", "Application Support", appname.TitleName(appName), "plugins",
	)
}

func windowsCacheDir(appName string) string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, appname.TitleName(appName), "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName, "plugins")
	}
	return filepath.Join(
		home, "AppData", "Local", appname.TitleName(appName), "plugins",
	)
}

func linuxCacheDir(appName string) string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName, "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName, "plugins")
	}
	return filepath.Join(
		home, ".local", "share", appName, "plugins",
	)
}
