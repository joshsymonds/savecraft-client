package pluginmgr

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

// PluginLoader compiles and registers a WASM plugin for a given game.
type PluginLoader interface {
	LoadPlugin(
		ctx context.Context,
		gameID string,
		wasmBytes, sigBytes []byte,
	) error
}

// Manager orchestrates plugin download, verification, caching, and loading.
type Manager struct {
	registry  Registry
	cache     *Cache
	loader    PluginLoader
	publicKey ed25519.PublicKey
	manifest  map[string]PluginInfo
	localDir  string
	logger    *slog.Logger
	mu        sync.Mutex
}

// NewManager creates a Manager with the given dependencies.
func NewManager(
	registry Registry,
	cache *Cache,
	loader PluginLoader,
	publicKey ed25519.PublicKey,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		registry:  registry,
		cache:     cache,
		loader:    loader,
		publicKey: publicKey,
		logger:    logger,
	}
}

// SetLocalDir configures a local directory override for plugin loading.
// Plugins found here are loaded directly instead of being downloaded.
func (m *Manager) SetLocalDir(dir string) {
	m.localDir = dir
}

// Manifests returns the plugin manifest, fetching it if not already cached.
func (m *Manager) Manifests(
	ctx context.Context,
) (map[string]PluginInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.manifest != nil {
		return m.manifest, nil
	}

	manifest, err := m.registry.FetchManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	m.manifest = manifest
	return manifest, nil
}

// EnsurePlugin guarantees the plugin for gameID is loaded into the runner.
// It checks local dir, then cache, then downloads from registry.
func (m *Manager) EnsurePlugin(ctx context.Context, gameID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check local directory override first.
	if m.localDir != "" {
		wasmPath := filepath.Join(m.localDir, "parser.wasm")
		if _, err := os.Stat(wasmPath); err == nil {
			return m.loadFromLocal(ctx, gameID, wasmPath)
		}
	}

	info, err := m.resolveManifestEntry(ctx, gameID)
	if err != nil {
		return err
	}

	// Check cache.
	if m.cache.HasVersion(gameID, info.Version) {
		wasm, sig, _, readErr := m.cache.Read(gameID)
		if readErr == nil {
			m.logger.InfoContext(
				ctx, "loading plugin from cache",
				slog.String("game_id", gameID),
				slog.String("version", info.Version),
			)
			return m.loadPlugin(ctx, gameID, wasm, sig)
		}
		// Cache read failed, fall through to download.
	}

	// Version differs but binary might be identical — check SHA256.
	if cachedHash := m.cache.SHA256(gameID); cachedHash != "" && cachedHash == info.SHA256 {
		if updateErr := m.cache.UpdateVersion(gameID, info.Version, info.SHA256); updateErr != nil {
			m.logger.WarnContext(ctx, "failed to update cached version",
				slog.String("game_id", gameID),
				slog.String("error", updateErr.Error()),
			)
		} else {
			m.logger.InfoContext(ctx, "plugin binary unchanged, skipping download",
				slog.String("game_id", gameID),
				slog.String("version", info.Version),
			)
			wasm, sig, _, readErr := m.cache.Read(gameID)
			if readErr == nil {
				return m.loadPlugin(ctx, gameID, wasm, sig)
			}
		}
	}

	return m.downloadAndLoad(ctx, gameID, info)
}

// CheckForUpdates compares cached versions against the manifest
// and re-downloads stale plugins.
func (m *Manager) CheckForUpdates(
	ctx context.Context,
) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	manifest, err := m.registry.FetchManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	m.manifest = manifest

	var updated []string
	for gameID, info := range manifest {
		if m.cache.HasVersion(gameID, info.Version) {
			continue
		}

		// Check if we have any version cached (stale version).
		_, _, cachedVersion, readErr := m.cache.Read(gameID)
		if readErr != nil {
			// Not cached at all, skip -- EnsurePlugin handles first download.
			continue
		}

		// Binary might be identical despite version change — check SHA256.
		if cachedHash := m.cache.SHA256(gameID); cachedHash != "" && cachedHash == info.SHA256 {
			if updateErr := m.cache.UpdateVersion(gameID, info.Version, info.SHA256); updateErr != nil {
				m.logger.WarnContext(ctx, "failed to update cached version",
					slog.String("game_id", gameID),
					slog.String("error", updateErr.Error()),
				)
			} else {
				m.logger.InfoContext(ctx, "plugin binary unchanged, skipping download",
					slog.String("game_id", gameID),
					slog.String("from", cachedVersion),
					slog.String("to", info.Version),
				)
				continue
			}
		}

		if m.updatePlugin(ctx, gameID, info, cachedVersion) {
			updated = append(updated, gameID)
		}
	}

	return updated, nil
}

func (m *Manager) resolveManifestEntry(
	ctx context.Context, gameID string,
) (PluginInfo, error) {
	if m.manifest == nil {
		manifest, err := m.registry.FetchManifest(ctx)
		if err != nil {
			return PluginInfo{}, fmt.Errorf("fetch manifest: %w", err)
		}
		m.manifest = manifest
	}

	info, ok := m.manifest[gameID]
	if !ok {
		return PluginInfo{}, fmt.Errorf("unknown plugin: %s", gameID)
	}
	return info, nil
}

func (m *Manager) downloadAndLoad(
	ctx context.Context, gameID string, info PluginInfo,
) error {
	m.logger.InfoContext(
		ctx, "downloading plugin",
		slog.String("game_id", gameID),
		slog.String("version", info.Version),
	)

	wasm, err := m.registry.Download(ctx, info.URL)
	if err != nil {
		return fmt.Errorf("download plugin %s: %w", gameID, err)
	}

	sig, err := m.registry.Download(ctx, info.URL+".sig")
	if err != nil {
		return fmt.Errorf("download plugin sig %s: %w", gameID, err)
	}

	if verifyErr := m.verifyPlugin(gameID, wasm, sig, info.SHA256); verifyErr != nil {
		return verifyErr
	}

	if cacheErr := m.cache.Write(gameID, info.Version, wasm, sig); cacheErr != nil {
		m.logger.WarnContext(
			ctx, "failed to cache plugin",
			slog.String("game_id", gameID),
			slog.String("error", cacheErr.Error()),
		)
	}

	m.logger.InfoContext(
		ctx, "loading plugin",
		slog.String("game_id", gameID),
		slog.String("version", info.Version),
	)
	return m.loadPlugin(ctx, gameID, wasm, sig)
}

func (m *Manager) verifyPlugin(
	gameID string, wasm, sig []byte, expectedHash string,
) error {
	if m.publicKey != nil {
		if verifyErr := signing.Verify(m.publicKey, wasm, sig); verifyErr != nil {
			return fmt.Errorf(
				"verify plugin %s: %w", gameID, verifyErr,
			)
		}
	}

	hash := sha256.Sum256(wasm)
	got := fmt.Sprintf("%x", hash)
	if got != expectedHash {
		return fmt.Errorf(
			"sha256 mismatch for %s: got %s, want %s",
			gameID, got, expectedHash,
		)
	}
	return nil
}

func (m *Manager) updatePlugin(
	ctx context.Context,
	gameID string,
	info PluginInfo,
	cachedVersion string,
) bool {
	m.logger.InfoContext(ctx, "updating plugin",
		slog.String("game_id", gameID),
		slog.String("from", cachedVersion),
		slog.String("to", info.Version),
	)

	wasm, dlErr := m.registry.Download(ctx, info.URL)
	if dlErr != nil {
		m.logger.ErrorContext(
			ctx, "failed to download update",
			slog.String("game_id", gameID),
			slog.String("error", dlErr.Error()),
		)
		return false
	}

	sig, dlErr := m.registry.Download(ctx, info.URL+".sig")
	if dlErr != nil {
		m.logger.ErrorContext(
			ctx, "failed to download update sig",
			slog.String("game_id", gameID),
			slog.String("error", dlErr.Error()),
		)
		return false
	}

	if verifyErr := m.verifyPlugin(gameID, wasm, sig, info.SHA256); verifyErr != nil {
		m.logger.ErrorContext(
			ctx, "update verification failed",
			slog.String("game_id", gameID),
			slog.String("error", verifyErr.Error()),
		)
		return false
	}

	if writeErr := m.cache.Write(gameID, info.Version, wasm, sig); writeErr != nil {
		m.logger.ErrorContext(
			ctx, "failed to cache update",
			slog.String("game_id", gameID),
			slog.String("error", writeErr.Error()),
		)
		return false
	}

	if loadErr := m.loadPlugin(ctx, gameID, wasm, sig); loadErr != nil {
		m.logger.ErrorContext(
			ctx, "failed to load update",
			slog.String("game_id", gameID),
			slog.String("error", loadErr.Error()),
		)
		return false
	}

	return true
}

func (m *Manager) loadPlugin(
	ctx context.Context, gameID string, wasm, sig []byte,
) error {
	if err := m.loader.LoadPlugin(ctx, gameID, wasm, sig); err != nil {
		return fmt.Errorf("load plugin %s: %w", gameID, err)
	}
	return nil
}

func (m *Manager) loadFromLocal(
	ctx context.Context, gameID, wasmPath string,
) error {
	wasm, err := os.ReadFile(filepath.Clean(wasmPath))
	if err != nil {
		return fmt.Errorf("read local plugin %s: %w", gameID, err)
	}

	var sig []byte
	sigPath := wasmPath + ".sig"
	sigData, sigErr := os.ReadFile(filepath.Clean(sigPath))
	if sigErr == nil {
		sig = sigData
	} else if !os.IsNotExist(sigErr) {
		return fmt.Errorf("read local sig %s: %w", gameID, sigErr)
	}

	if m.publicKey != nil && sig != nil {
		if verifyErr := signing.Verify(m.publicKey, wasm, sig); verifyErr != nil {
			return fmt.Errorf(
				"verify local plugin %s: %w", gameID, verifyErr,
			)
		}
	}

	m.logger.InfoContext(
		ctx, "loading local plugin",
		slog.String("game_id", gameID),
		slog.String("path", wasmPath),
	)
	return m.loadPlugin(ctx, gameID, wasm, sig)
}
