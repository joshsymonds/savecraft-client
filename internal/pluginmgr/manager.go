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
	"github.com/joshsymonds/savecraft.gg/internal/version"
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
	registry    Registry
	cache       *Cache
	loader      PluginLoader
	publicKey   ed25519.PublicKey
	manifest    map[string]PluginInfo
	localDir    string
	localHashes map[string]string // SHA-256 of last-loaded local WASM per gameID
	logger      *slog.Logger
	mu          sync.Mutex
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

	// Check local directory override first (per-game subdirectory).
	if m.localDir != "" {
		wasmPath := filepath.Join(m.localDir, gameID, "parser.wasm")
		if _, err := os.Stat(wasmPath); err == nil {
			return m.loadFromLocal(ctx, gameID, wasmPath, nil)
		}
	}

	info, err := m.resolveManifestEntry(ctx, gameID)
	if err != nil {
		return err
	}

	// Check cache (exact version match).
	if m.cache.HasVersion(gameID, info.Version) {
		if handled, cacheErr := m.loadFromCache(ctx, gameID, info); handled {
			return cacheErr
		}
		// Cache unreadable — fall through to download.
	}

	// Version differs but the binary might be identical — check SHA256.
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
			if handled, cacheErr := m.loadFromCache(ctx, gameID, info); handled {
				return cacheErr
			}
		}
	}

	if m.isRollback(gameID, info) {
		m.logger.WarnContext(ctx, "refusing plugin: manifest version older than cached (anti-rollback)",
			slog.String("game_id", gameID),
			slog.String("manifest_version", info.Version),
		)
		return fmt.Errorf(
			"refusing plugin %s: manifest version %q is older than the cached version (anti-rollback)",
			gameID, info.Version,
		)
	}

	return m.downloadAndLoad(ctx, gameID, info)
}

// isRollback reports whether adopting info would downgrade an already-cached
// plugin: the signed manifest (verified upstream — its Version is authentic)
// offers a strictly OLDER version than what is cached. First install (no
// cache), an equal/newer version, or an identical artifact (same sha256, just
// re-tagged) are not rollbacks. Empty/garbage manifest versions compare as
// not-newer, so they are treated as a rollback (fail closed). This is the
// lightweight, proportionate plugin anti-rollback (epic R8 plugin half): it
// defends the channel/replay adversary; a local-FS attacker who can rewrite
// the cache is out of scope by decision (sandbox + signature verification
// cap the blast radius).
func (m *Manager) isRollback(gameID string, info PluginInfo) bool {
	_, _, cachedVersion, readErr := m.cache.Read(gameID)
	if readErr != nil {
		return false // no cached anchor — first install
	}
	if cachedHash := m.cache.SHA256(gameID); cachedHash != "" && cachedHash == info.SHA256 {
		return false // identical artifact, just re-tagged — not a downgrade
	}
	return version.IsNewer(cachedVersion, info.Version)
}

// CheckForUpdates compares cached versions against the manifest
// and re-downloads stale plugins. When localDir is set, local plugins
// are checked for on-disk changes first; remote plugins are checked after.
func (m *Manager) CheckForUpdates(
	ctx context.Context,
) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var updated []string
	localGames := map[string]bool{}

	// Check local directory plugins for changes.
	if m.localDir != "" {
		localUpdated := m.checkLocalPlugins(ctx)
		for _, gameID := range localUpdated {
			localGames[gameID] = true
		}
		updated = append(updated, localUpdated...)
	}

	// Check remote plugins (skip games handled locally).
	if m.registry != nil {
		remoteUpdated, err := m.checkRemotePlugins(ctx, localGames)
		if err != nil {
			return updated, err
		}
		updated = append(updated, remoteUpdated...)
	}

	return updated, nil
}

// checkRemotePlugins fetches the remote manifest and re-downloads stale plugins,
// skipping any gameIDs in the skip set (already handled locally). Must be called with mu held.
func (m *Manager) checkRemotePlugins(
	ctx context.Context, skip map[string]bool,
) ([]string, error) {
	manifest, err := m.registry.FetchManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	m.manifest = manifest

	var updated []string
	for gameID, info := range manifest {
		if skip[gameID] {
			continue
		}

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

		if m.isRollback(gameID, info) {
			m.logger.WarnContext(ctx,
				"skipping plugin update: manifest version older than cached (anti-rollback)",
				slog.String("game_id", gameID),
				slog.String("from", cachedVersion),
				slog.String("manifest_version", info.Version),
			)
			continue
		}

		if m.updatePlugin(ctx, gameID, info, cachedVersion) {
			updated = append(updated, gameID)
		}
	}
	return updated, nil
}

// checkLocalPlugins scans localDir for plugin subdirectories and reloads
// any whose WASM file has changed since last load. Must be called with mu held.
func (m *Manager) checkLocalPlugins(ctx context.Context) []string {
	entries, err := os.ReadDir(m.localDir)
	if err != nil {
		m.logger.WarnContext(ctx, "failed to read local plugin dir",
			slog.String("path", m.localDir),
			slog.String("error", err.Error()),
		)
		return nil
	}

	var updated []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gameID := entry.Name()
		wasmPath := filepath.Join(m.localDir, gameID, "parser.wasm")

		wasmBytes, readErr := os.ReadFile(filepath.Clean(wasmPath))
		if readErr != nil {
			continue // No parser.wasm in this subdir, skip.
		}

		hash := sha256.Sum256(wasmBytes)
		hashStr := fmt.Sprintf("%x", hash)

		if m.localHashes != nil && m.localHashes[gameID] == hashStr {
			continue // Unchanged.
		}

		if loadErr := m.loadFromLocal(ctx, gameID, wasmPath, wasmBytes); loadErr != nil {
			m.logger.ErrorContext(ctx, "failed to reload local plugin",
				slog.String("game_id", gameID),
				slog.String("error", loadErr.Error()),
			)
			continue
		}
		updated = append(updated, gameID)
	}
	return updated
}

// loadFromCache reads gameID from the on-disk cache and loads it, verifying
// the cached bytes + signature against info BEFORE use (finding 1.1 / R4).
// handled=true means the cache satisfied the request — either successfully or
// with a fatal verification error returned in err (a poisoned/corrupt cache
// is fail-closed, never a silent re-download). handled=false means the cache
// could not be read and the caller should fall through to download.
func (m *Manager) loadFromCache(
	ctx context.Context, gameID string, info PluginInfo,
) (handled bool, err error) {
	wasm, sig, _, readErr := m.cache.Read(gameID)
	if readErr != nil {
		// A cache-read failure is not propagated: it means "cache miss",
		// and the caller falls through to a fresh (verified) download.
		return false, nil //nolint:nilerr // intentional: cache miss, not an error to surface
	}
	if verifyErr := m.verifyPlugin(gameID, wasm, sig, info.SHA256); verifyErr != nil {
		return true, verifyErr
	}
	m.logger.InfoContext(
		ctx, "loading plugin from cache",
		slog.String("game_id", gameID),
		slog.String("version", info.Version),
	)
	return true, m.loadPlugin(ctx, gameID, wasm, sig)
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
	if ok {
		return info, nil
	}

	// Game not in cached manifest — re-fetch in case a new plugin was
	// deployed after the daemon started.
	m.logger.InfoContext(ctx, "plugin not in cached manifest, re-fetching",
		slog.String("game_id", gameID),
	)
	manifest, err := m.registry.FetchManifest(ctx)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("fetch manifest: %w", err)
	}
	m.manifest = manifest

	info, ok = m.manifest[gameID]
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
	// Verification is unconditional. A nil/invalid key makes signing.Verify
	// fail closed — it is never a skip (epic R3).
	if verifyErr := signing.Verify(m.publicKey, wasm, sig); verifyErr != nil {
		return fmt.Errorf(
			"verify plugin %s: %w", gameID, verifyErr,
		)
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

// loadFromLocal loads a plugin from the local directory. When preReadWasm is
// non-nil, it is used directly instead of reading the file from disk (avoids
// a double read when the caller already read the file for hash comparison).
func (m *Manager) loadFromLocal(
	ctx context.Context, gameID, wasmPath string, preReadWasm []byte,
) error {
	wasm := preReadWasm
	if wasm == nil {
		var err error
		wasm, err = os.ReadFile(filepath.Clean(wasmPath))
		if err != nil {
			return fmt.Errorf("read local plugin %s: %w", gameID, err)
		}
	}

	sigPath := wasmPath + ".sig"
	sig, sigErr := os.ReadFile(filepath.Clean(sigPath))
	if sigErr != nil {
		if os.IsNotExist(sigErr) {
			// A local plugin without a signature is a hard error — there is
			// no unsigned execution path (finding 1.2 / R5). Sign dev builds
			// with cmd/savecraft-sign.
			return fmt.Errorf(
				"local plugin %s: missing required signature %s — sign it with cmd/savecraft-sign",
				gameID, sigPath,
			)
		}
		return fmt.Errorf("read local sig %s: %w", gameID, sigErr)
	}

	// Verification is unconditional; a nil/invalid key fails closed (epic R3).
	if verifyErr := signing.Verify(m.publicKey, wasm, sig); verifyErr != nil {
		return fmt.Errorf(
			"verify local plugin %s: %w", gameID, verifyErr,
		)
	}

	m.logger.InfoContext(
		ctx, "loading local plugin",
		slog.String("game_id", gameID),
		slog.String("path", wasmPath),
	)
	if loadErr := m.loadPlugin(ctx, gameID, wasm, sig); loadErr != nil {
		return loadErr
	}

	// Track hash for change detection in CheckForUpdates.
	hash := sha256.Sum256(wasm)
	if m.localHashes == nil {
		m.localHashes = make(map[string]string)
	}
	m.localHashes[gameID] = fmt.Sprintf("%x", hash)
	return nil
}
