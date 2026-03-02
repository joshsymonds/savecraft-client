// Package daemon coordinates file watching, plugin execution, and server communication.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
)

const pluginUpdateInterval = 24 * time.Hour
const selfUpdateInterval = 6 * time.Hour

// --- Domain types ---

// GameState is the structured output from parsing a save file.
type GameState struct {
	Identity Identity           `json:"identity"`
	Summary  string             `json:"summary"`
	Sections map[string]Section `json:"sections"`
}

// Identity identifies a specific save within a game.
type Identity struct {
	SaveName string         `json:"saveName,omitempty"`
	GameID   string         `json:"gameId"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Section is a named block of game state data.
type Section struct {
	Description string `json:"description"`
	Data        any    `json:"data"`
}

// PluginError is returned when a WASM plugin fails to parse a save file.
type PluginError struct {
	Type       string `json:"errorType"`
	Message    string `json:"message"`
	ByteOffset int64  `json:"byteOffset,omitempty"`
}

func (e *PluginError) Error() string { return e.Message }

// PushStatusError is returned when the server returns an HTTP error.
// It lives in the daemon package (not push) to avoid circular deps.
type PushStatusError struct {
	StatusCode int
	Body       string
}

func (e *PushStatusError) Error() string {
	return fmt.Sprintf("push returned status %d: %s", e.StatusCode, e.Body)
}

// --- Events and results ---

// FileEvent represents a filesystem change notification.
// Data optionally carries the file contents already read by the watcher
// (for SHA-256 dedup). When non-nil the daemon skips a second ReadFile call.
type FileEvent struct {
	Path string
	Op   FileOp
	Data []byte
}

// FileOp describes the type of filesystem operation.
type FileOp int

// File operation constants.
const (
	FileCreate FileOp = iota
	FileModify
	FileRemove
)

// PushResult is the server response after a successful save push.
type PushResult struct {
	SaveUUID          string `json:"saveUuid"`
	SnapshotTimestamp string `json:"snapshotTimestamp"`
}

// --- Configuration ---

// RetryConfig controls exponential backoff for push retries.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

const (
	httpStatusServer = 500
	maxBitShift      = 62
)

// Config holds all daemon configuration.
type Config struct {
	ServerURL  string
	AuthToken  string `json:"-"`
	DeviceID   string
	Version    string
	BinaryPath string
	Retry      RetryConfig
	Games      map[string]GameConfig
}

// GameConfig holds per-game configuration.
type GameConfig struct {
	SavePath       string
	FileExtensions []string
	Enabled        bool
}

// --- Interfaces ---

// Watcher watches directories for file changes.
type Watcher interface {
	Add(path string) error
	Remove(path string) error
	Events() <-chan FileEvent
	Close() error
}

// Runner runs a WASM plugin to parse save file bytes.
type Runner interface {
	Run(
		ctx context.Context,
		gameID string,
		saveBytes []byte,
		onStatus func(string),
	) (*GameState, error)
}

// PushClient pushes parsed game state to the server.
type PushClient interface {
	Push(
		ctx context.Context,
		gameID string,
		body []byte,
		parsedAt time.Time,
	) (*PushResult, error)
}

// WSClient handles WebSocket communication with the server.
type WSClient interface {
	Connect(ctx context.Context) error
	Send(msg []byte) error
	Messages() <-chan []byte
	Close() error
}

// FS abstracts filesystem operations for testability.
type FS interface {
	Stat(path string) (fs.FileInfo, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	ReadFile(path string) ([]byte, error)
}

// PluginManager handles plugin download, verification, caching, and loading.
type PluginManager interface {
	EnsurePlugin(ctx context.Context, gameID string) error
	CheckForUpdates(ctx context.Context) ([]string, error)
	Manifests(ctx context.Context) (map[string]pluginmgr.PluginInfo, error)
}

// Updater checks for and applies daemon self-updates.
type Updater interface {
	Check(ctx context.Context, currentVersion, platform string) (*UpdateInfo, error)
	Apply(ctx context.Context, info *UpdateInfo, binaryPath string) error
}

// UpdateInfo describes an available daemon update.
type UpdateInfo struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	SignatureURL string `json:"signatureUrl"`
	SHA256       string `json:"sha256"`
}

// DiscoveredGame represents a game whose save directory was found on disk.
type DiscoveredGame struct {
	GameID    string `json:"gameId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	FileCount int    `json:"fileCount"`
}

// --- Daemon ---

// Daemon coordinates file watching, plugin execution, and server communication.
type Daemon struct {
	cfg     Config
	fs      FS
	watcher Watcher
	runner  Runner
	pusher  PushClient
	ws      WSClient
	plugins PluginManager
	updater Updater

	// exitFunc is called after a successful self-update to terminate
	// the process. Defaults to os.Exit; overridden in tests.
	exitFunc func(int)

	// Maps watched directory -> game ID.
	watchedDirs map[string]string
}

// New creates a Daemon with the given dependencies.
func New(
	cfg Config,
	fsys FS,
	watcher Watcher,
	runner Runner,
	pusher PushClient,
	ws WSClient,
	plugins PluginManager,
	updater Updater,
) *Daemon {
	return &Daemon{
		cfg:         cfg,
		fs:          fsys,
		watcher:     watcher,
		runner:      runner,
		pusher:      pusher,
		ws:          ws,
		plugins:     plugins,
		updater:     updater,
		exitFunc:    os.Exit,
		watchedDirs: make(map[string]string),
	}
}

// Run connects to the server and enters the main event loop.
// It blocks until ctx is canceled.
func (d *Daemon) Run(ctx context.Context) (runErr error) {
	if err := d.ws.Connect(ctx); err != nil {
		return fmt.Errorf("ws connect: %w", err)
	}
	defer func() {
		if closeErr := d.ws.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("ws close: %w", closeErr)
		}
	}()

	d.sendEvent("daemonOnline", map[string]any{
		"deviceId": d.cfg.DeviceID,
		"version":  d.cfg.Version,
		"platform": runtime.GOOS + "-" + runtime.GOARCH,
	})

	d.discoverGames(ctx)

	for gameID, gameCfg := range d.cfg.Games {
		if !gameCfg.Enabled {
			continue
		}
		if !d.ensurePluginReady(ctx, gameID) {
			continue
		}
		d.scanGame(ctx, gameID, gameCfg)
	}

	var updateTicker *time.Ticker
	var updateCh <-chan time.Time
	if d.plugins != nil {
		updateTicker = time.NewTicker(pluginUpdateInterval)
		updateCh = updateTicker.C
		defer updateTicker.Stop()
	}

	var selfUpdateTicker *time.Ticker
	var selfUpdateCh <-chan time.Time
	if d.updater != nil {
		selfUpdateTicker = time.NewTicker(selfUpdateInterval)
		selfUpdateCh = selfUpdateTicker.C
		defer selfUpdateTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			d.sendEvent("daemonOffline", map[string]any{
				"deviceId": d.cfg.DeviceID,
			})
			return nil
		case ev := <-d.watcher.Events():
			d.handleFileEvent(ctx, ev)
		case msg := <-d.ws.Messages():
			d.handleCommand(ctx, msg)
		case <-updateCh:
			d.checkPluginUpdates(ctx)
		case <-selfUpdateCh:
			d.checkSelfUpdate(ctx)
		}
	}
}

func (d *Daemon) checkSelfUpdate(ctx context.Context) {
	if d.updater == nil {
		return
	}
	info, err := d.updater.Check(ctx, d.cfg.Version, runtime.GOOS+"-"+runtime.GOARCH)
	if err != nil {
		return
	}
	if info == nil {
		return
	}
	d.applyDaemonUpdate(ctx, info)
}

func (d *Daemon) applyDaemonUpdate(ctx context.Context, info *UpdateInfo) {
	if d.updater == nil {
		return
	}
	d.sendEvent("daemonUpdateStarted", map[string]any{
		"version": info.Version,
	})
	if err := d.updater.Apply(ctx, info, d.cfg.BinaryPath); err != nil {
		d.sendEvent("daemonUpdateFailed", map[string]any{
			"version": info.Version,
			"message": err.Error(),
		})
		return
	}
	d.sendEvent("daemonOffline", map[string]any{
		"deviceId": d.cfg.DeviceID,
	})
	d.exitFunc(0)
}

func (d *Daemon) checkPluginUpdates(ctx context.Context) {
	updated, err := d.plugins.CheckForUpdates(ctx)
	if err != nil {
		d.sendEvent("pluginUpdateCheckFailed", map[string]any{
			"message": err.Error(),
		})
		return
	}
	for _, gameID := range updated {
		d.sendEvent("pluginUpdated", map[string]any{
			"gameId": gameID,
		})
	}
}

// ensurePluginReady downloads/verifies the plugin for gameID if a
// PluginManager is configured. Returns true if the plugin is ready.
func (d *Daemon) ensurePluginReady(
	ctx context.Context, gameID string,
) bool {
	if d.plugins == nil {
		return true
	}
	if ensureErr := d.plugins.EnsurePlugin(ctx, gameID); ensureErr != nil {
		d.sendEvent("pluginDownloadFailed", map[string]any{
			"gameId":  gameID,
			"message": ensureErr.Error(),
		})
		return false
	}
	return true
}

func (d *Daemon) discoverGames(ctx context.Context) {
	if d.plugins == nil {
		return
	}

	manifests, err := d.plugins.Manifests(ctx)
	if err != nil {
		return
	}

	var discovered []DiscoveredGame
	for gameID, info := range manifests {
		pathTemplate, ok := info.DefaultPaths[runtime.GOOS]
		if !ok || pathTemplate == "" {
			continue
		}

		path := expandPath(pathTemplate)
		stat, statErr := d.fs.Stat(path)
		if statErr != nil || !stat.IsDir() {
			continue
		}

		entries, readErr := d.fs.ReadDir(path)
		if readErr != nil {
			continue
		}

		matching := d.filterByExtension(entries, info.FileExtensions)
		discovered = append(discovered, DiscoveredGame{
			GameID:    gameID,
			Name:      info.Name,
			Path:      path,
			FileCount: len(matching),
		})
	}

	d.sendEvent("gamesDiscovered", map[string]any{
		"games": discovered,
	})
}

func (d *Daemon) scanGame(
	ctx context.Context, gameID string, cfg GameConfig,
) {
	d.sendEvent("scanStarted", map[string]any{
		"gameId": gameID,
		"path":   cfg.SavePath,
	})

	info, err := d.fs.Stat(cfg.SavePath)
	if err != nil || !info.IsDir() {
		d.sendEvent("gameNotFound", map[string]any{
			"gameId":       gameID,
			"pathsChecked": []string{cfg.SavePath},
		})
		return
	}

	entries, err := d.fs.ReadDir(cfg.SavePath)
	if err != nil {
		d.sendEvent("gameNotFound", map[string]any{
			"gameId":       gameID,
			"pathsChecked": []string{cfg.SavePath},
		})
		return
	}

	matchingFiles := d.filterByExtension(entries, cfg.FileExtensions)

	d.sendEvent("scanCompleted", map[string]any{
		"gameId":     gameID,
		"path":       cfg.SavePath,
		"filesFound": len(matchingFiles),
		"fileNames":  matchingFiles,
	})

	if len(matchingFiles) == 0 {
		d.sendEvent("gameNotFound", map[string]any{
			"gameId":       gameID,
			"pathsChecked": []string{cfg.SavePath},
		})
		return
	}

	d.sendEvent("gameDetected", map[string]any{
		"gameId":    gameID,
		"path":      cfg.SavePath,
		"saveCount": len(matchingFiles),
	})

	if watchErr := d.watcher.Add(cfg.SavePath); watchErr != nil {
		return
	}
	d.watchedDirs[cfg.SavePath] = gameID

	d.sendEvent("watching", map[string]any{
		"gameId":         gameID,
		"path":           cfg.SavePath,
		"filesMonitored": len(matchingFiles),
	})

	for _, fileName := range matchingFiles {
		fullPath := filepath.Join(cfg.SavePath, fileName)
		d.parseAndPush(ctx, gameID, fullPath, fileName, nil)
	}
}

func (d *Daemon) handleFileEvent(ctx context.Context, ev FileEvent) {
	if ev.Op == FileRemove {
		return
	}

	dir := filepath.Dir(ev.Path)
	gameID, ok := d.watchedDirs[dir]
	if !ok {
		return
	}

	gameCfg := d.cfg.Games[gameID]
	ext := filepath.Ext(ev.Path)
	if !matchesExtension(ext, gameCfg.FileExtensions) {
		return
	}

	fileName := filepath.Base(ev.Path)
	d.parseAndPush(ctx, gameID, ev.Path, fileName, ev.Data)
}

// parseAndPush reads the save file, runs the plugin, and pushes the result.
// When preloadedData is non-nil (e.g. from the watcher's SHA-256 read), it is
// used directly, avoiding a redundant filesystem read.
func (d *Daemon) parseAndPush(
	ctx context.Context, gameID, fullPath, fileName string,
	preloadedData []byte,
) {
	d.sendEvent("parseStarted", map[string]any{
		"gameId":   gameID,
		"fileName": fileName,
	})

	saveBytes := preloadedData
	if saveBytes == nil {
		var err error
		saveBytes, err = d.fs.ReadFile(fullPath)
		if err != nil {
			d.sendEvent("parseFailed", map[string]any{
				"gameId":    gameID,
				"fileName":  fileName,
				"errorType": "PARSE_ERROR_TYPE_PARSE_ERROR",
				"message":   fmt.Sprintf("read file: %v", err),
			})
			return
		}
	}

	onStatus := func(message string) {
		d.sendEvent("pluginStatus", map[string]any{
			"gameId":   gameID,
			"fileName": fileName,
			"message":  message,
		})
	}

	state, err := d.runner.Run(ctx, gameID, saveBytes, onStatus)
	if err != nil {
		errorType := "PARSE_ERROR_TYPE_PARSE_ERROR"
		if pluginErr, ok := errors.AsType[*PluginError](err); ok {
			errorType = pluginErr.Type
		}
		d.sendEvent("parseFailed", map[string]any{
			"gameId":    gameID,
			"fileName":  fileName,
			"errorType": errorType,
			"message":   err.Error(),
		})
		return
	}

	stateJSON, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		d.sendEvent("parseFailed", map[string]any{
			"gameId":    gameID,
			"fileName":  fileName,
			"errorType": "PARSE_ERROR_TYPE_PARSE_ERROR",
			"message":   fmt.Sprintf("marshal state: %v", marshalErr),
		})
		return
	}

	d.sendEvent("parseCompleted", map[string]any{
		"gameId":        gameID,
		"fileName":      fileName,
		"identity":      state.Identity,
		"summary":       state.Summary,
		"sectionsCount": len(state.Sections),
		"sizeBytes":     len(stateJSON),
	})

	d.sendEvent("pushStarted", map[string]any{
		"gameId":    gameID,
		"summary":   state.Summary,
		"sizeBytes": len(stateJSON),
	})

	parsedAt := time.Now().UTC()
	result, err := d.pushWithRetry(ctx, gameID, state.Summary, parsedAt, stateJSON)
	if err != nil {
		// pushWithRetry already emitted pushFailed for retryable errors.
		// For non-retryable errors, emit pushFailed here.
		if !isPushRetryable(err) {
			d.sendEvent("pushFailed", map[string]any{
				"gameId":    gameID,
				"message":   err.Error(),
				"willRetry": false,
			})
		}
		return
	}

	d.sendEvent("pushCompleted", map[string]any{
		"gameId":            gameID,
		"saveUuid":          result.SaveUUID,
		"summary":           state.Summary,
		"snapshotSizeBytes": len(stateJSON),
		"identity":          state.Identity,
	})
}

// isPushRetryable returns true for errors that may succeed on retry.
func isPushRetryable(err error) bool {
	if statusErr, ok := errors.AsType[*PushStatusError](err); ok {
		return statusErr.StatusCode >= httpStatusServer
	}
	// Connection errors (no PushStatusError) are retryable.
	return true
}

func (d *Daemon) pushWithRetry(
	ctx context.Context,
	gameID string,
	summary string,
	parsedAt time.Time,
	stateJSON []byte,
) (*PushResult, error) {
	maxAttempts := max(d.cfg.Retry.MaxRetries+1, 1)

	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			shift := uint(min(attempt-1, maxBitShift)) //nolint:gosec // attempt is always small
			delay := min(d.cfg.Retry.BaseDelay<<shift, d.cfg.Retry.MaxDelay)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("push retry interrupted: %w", ctx.Err())
			case <-time.After(delay):
			}
			// Re-emit pushStarted on retry.
			d.sendEvent("pushStarted", map[string]any{
				"gameId":    gameID,
				"summary":   summary,
				"sizeBytes": len(stateJSON),
			})
		}

		result, pushErr := d.pusher.Push(ctx, gameID, stateJSON, parsedAt)
		if pushErr == nil {
			return result, nil
		}
		lastErr = pushErr

		if !isPushRetryable(pushErr) {
			return nil, fmt.Errorf("push: %w", pushErr)
		}

		willRetry := attempt < maxAttempts-1
		d.sendEvent("pushFailed", map[string]any{
			"gameId":    gameID,
			"message":   pushErr.Error(),
			"willRetry": willRetry,
		})
	}
	return nil, lastErr
}

func (d *Daemon) handleCommand(ctx context.Context, data []byte) {
	cmdType, payload, parseErr := parseMessage(data)
	if parseErr != nil {
		return
	}

	switch cmdType {
	case "configUpdate":
		d.handleConfigUpdate(ctx, payload)
	case "rescanGame":
		var cmd struct {
			GameID string `json:"gameId"`
		}
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return
		}
		if gameCfg, ok := d.cfg.Games[cmd.GameID]; ok {
			d.scanGame(ctx, cmd.GameID, gameCfg)
		}
	case "testPath":
		var cmd struct {
			GameID string `json:"gameId"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return
		}
		d.handleTestPath(cmd.GameID, cmd.Path)
	case "discoverGames":
		d.discoverGames(ctx)
	case "daemonUpdateAvailable":
		var info UpdateInfo
		if err := json.Unmarshal(payload, &info); err != nil {
			return
		}
		d.applyDaemonUpdate(ctx, &info)
	}
}

func (d *Daemon) handleConfigUpdate(
	ctx context.Context, payload json.RawMessage,
) {
	var update struct {
		Games map[string]struct {
			SavePath       string   `json:"savePath"`
			Enabled        bool     `json:"enabled"`
			FileExtensions []string `json:"fileExtensions"`
		} `json:"games"`
	}
	if err := json.Unmarshal(payload, &update); err != nil {
		return
	}

	d.removeStaleGames(update.Games)

	for gameID, newGame := range update.Games {
		gameCfg := GameConfig{
			SavePath:       newGame.SavePath,
			Enabled:        newGame.Enabled,
			FileExtensions: newGame.FileExtensions,
		}

		oldCfg, existed := d.cfg.Games[gameID]
		d.cfg.Games[gameID] = gameCfg

		switch {
		case !newGame.Enabled:
			if existed {
				d.unwatchGame(oldCfg.SavePath)
			}
		case !existed || !oldCfg.Enabled:
			if !d.ensurePluginReady(ctx, gameID) {
				delete(d.cfg.Games, gameID)
				continue
			}
			d.scanGame(ctx, gameID, gameCfg)
		case oldCfg.SavePath != newGame.SavePath:
			d.unwatchGame(oldCfg.SavePath)
			if !d.ensurePluginReady(ctx, gameID) {
				delete(d.cfg.Games, gameID)
				continue
			}
			d.scanGame(ctx, gameID, gameCfg)
		}
	}
}

func (d *Daemon) removeStaleGames(newGames map[string]struct {
	SavePath       string   `json:"savePath"`
	Enabled        bool     `json:"enabled"`
	FileExtensions []string `json:"fileExtensions"`
},
) {
	newGameIDs := make(map[string]bool, len(newGames))
	for gameID := range newGames {
		newGameIDs[gameID] = true
	}

	for gameID, oldCfg := range d.cfg.Games {
		if !newGameIDs[gameID] {
			d.unwatchGame(oldCfg.SavePath)
			delete(d.cfg.Games, gameID)
		}
	}
}

func (d *Daemon) unwatchGame(savePath string) {
	if _, ok := d.watchedDirs[savePath]; !ok {
		return
	}
	if removeErr := d.watcher.Remove(savePath); removeErr != nil {
		// Path may already be gone; clean up internal state regardless.
		_ = removeErr
	}
	delete(d.watchedDirs, savePath)
}

func (d *Daemon) handleTestPath(gameID, path string) {
	info, err := d.fs.Stat(path)
	if err != nil || !info.IsDir() {
		d.sendEvent("testPathResult", map[string]any{
			"gameId":     gameID,
			"path":       path,
			"valid":      false,
			"filesFound": 0,
		})
		return
	}

	entries, err := d.fs.ReadDir(path)
	if err != nil {
		d.sendEvent("testPathResult", map[string]any{
			"gameId":     gameID,
			"path":       path,
			"valid":      false,
			"filesFound": 0,
		})
		return
	}

	gameCfg := d.cfg.Games[gameID]
	fileNames := d.filterByExtension(entries, gameCfg.FileExtensions)

	d.sendEvent("testPathResult", map[string]any{
		"gameId":     gameID,
		"path":       path,
		"valid":      len(fileNames) > 0,
		"filesFound": len(fileNames),
		"fileNames":  fileNames,
	})
}

func (d *Daemon) filterByExtension(
	entries []fs.DirEntry, extensions []string,
) []string {
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if matchesExtension(ext, extensions) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func matchesExtension(ext string, extensions []string) bool {
	for _, want := range extensions {
		if strings.EqualFold(ext, want) {
			return true
		}
	}
	return false
}

func (d *Daemon) sendEvent(eventType string, payload any) {
	msg := map[string]any{eventType: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if sendErr := d.ws.Send(data); sendErr != nil {
		return
	}
}

func parseMessage(
	data []byte,
) (string, json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", nil, fmt.Errorf(
			"unmarshal message envelope: %w", err,
		)
	}
	for key, val := range envelope {
		return key, val, nil
	}
	return "", nil, fmt.Errorf("empty message")
}
