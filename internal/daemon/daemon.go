// Package daemon coordinates file watching, plugin execution, and server communication.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
)

const pluginUpdateInterval = 24 * time.Hour
const selfUpdateInterval = 6 * time.Hour
const heartbeatInterval = 30 * time.Second

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
// Data must be a JSON object (not an array or scalar).
type Section struct {
	Description string         `json:"description"`
	Data        jsontext.Value `json:"data"`
}

// PluginError is returned when a WASM plugin fails to parse a save file.
type PluginError struct {
	Type       string `json:"errorType"`
	Message    string `json:"message"`
	ByteOffset int64  `json:"byteOffset,omitempty"`
}

func (e *PluginError) Error() string { return e.Message }

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

// --- Configuration ---

// Config holds all daemon configuration.
type Config struct {
	ServerURL      string
	AuthToken      string `json:"-"`
	SourceID       string
	SourceUUID     string
	Version        string
	BinaryPath     string
	TrayBinaryPath string
	Games          map[string]GameConfig
}

// GameConfig holds per-game configuration.
type GameConfig struct {
	SavePath       string   `json:"savePath"`
	FileExtensions []string `json:"fileExtensions"`
	FilePatterns   []string `json:"filePatterns,omitempty"`
	ExcludeDirs    []string `json:"excludeDirs,omitempty"`
	ExcludeSaves   []string `json:"excludeSaves,omitempty"`
	Enabled        bool     `json:"enabled"`
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
		fileName string,
		saveBytes []byte,
		onStatus func(string),
	) (*GameState, error)
}

// WSClient handles WebSocket communication with the server.
type WSClient interface {
	Connect(ctx context.Context) error
	Send(msg []byte) error
	Messages() <-chan []byte
	Reconnected() <-chan struct{}
	ForceReconnect()
	Close() error
	Connected() bool
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
	Check(ctx context.Context, currentVersion, platform string) (*CheckResult, error)
	Apply(ctx context.Context, info *UpdateInfo, binaryPath string) error
}

// UpdateInfo describes an available update for a single binary.
type UpdateInfo struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	SignatureURL string `json:"signatureUrl"`
	SHA256       string `json:"sha256"`
}

// CheckResult holds the result of checking for updates.
// Daemon is always populated when an update is available.
// Tray is populated when a tray binary update is available in the manifest.
type CheckResult struct {
	Daemon *UpdateInfo
	Tray   *UpdateInfo
}

// DiscoveredGame represents a game whose save directory was found on disk.
type DiscoveredGame struct {
	GameID         string   `json:"gameId"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	FileCount      int      `json:"fileCount"`
	FileExtensions []string `json:"fileExtensions"`
	FilePatterns   []string `json:"filePatterns,omitempty"`
	ExcludeDirs    []string `json:"excludeDirs,omitempty"`
}

// --- Daemon ---

// LinkCallbacks lets the boot flow receive link state changes from the daemon.
type LinkCallbacks struct {
	OnLinked   func()
	OnLinkCode func(code string, expiresAt time.Time)
}

// Daemon coordinates file watching, plugin execution, and server communication.
type Daemon struct {
	cfg     Config
	fs      FS
	watcher Watcher
	runner  Runner
	ws      WSClient
	plugins PluginManager
	updater Updater
	log     *slog.Logger

	// exitFunc is called after a successful self-update to terminate
	// the process. Defaults to os.Exit; overridden in tests.
	exitFunc func(int)

	// restartFunc is called before exitFunc to spawn the new daemon binary.
	// On Windows, this spawns a new process; on Linux, systemd handles restart.
	// Defaults to a no-op; set by the boot flow in cmd/savecraftd.
	restartFunc func(daemonPath, trayPath string) error

	// mu protects watchedDirs, cfg.Games, and link state from concurrent access.
	mu sync.RWMutex

	// Maps watched directory -> game ID.
	watchedDirs map[string]string

	// configDir is the directory for persisting config cache.
	// Defaults to os.UserConfigDir()/savecraft; empty disables caching.
	configDir string

	startTime time.Time

	// Link state: the daemon starts with unknown link state. The server
	// pushes SourceLinked (if linked) or RefreshLinkCodeResult (if not)
	// after the daemon sends SourceOnline.
	linked     bool
	linkCode   string
	linkExpiry time.Time
	linkCB     LinkCallbacks

	// pendingLinkCode receives the result of an UnlinkSource or RefreshLinkCode
	// request, allowing synchronous callers (like the repair endpoint) to block
	// until the server responds.
	pendingLinkCode chan linkCodeResult

	// pluginUpdateResetCh signals the event loop to reset the plugin update ticker.
	// Sent by UpdatePlugins (local API callback) and handlePluginAvailable.
	pluginUpdateResetCh chan struct{}

	// pluginReloadCh receives game IDs from the PluginWatcher when a local
	// plugin WASM file changes on disk. Nil when local plugin dir is not set.
	pluginReloadCh <-chan string

	// lastPushedSectionHashes caches per-section SHA-256 hashes of the last
	// successfully pushed GameSection proto bytes, keyed by file path then
	// section name. On re-parse, only sections whose hash changed are included
	// in the PushSave. If no sections changed, the push is skipped entirely.
	lastPushedSectionHashes map[string]map[string][32]byte

	// hasAnnounced is set after the first announceOnline completes.
	// On subsequent calls (reconnects), discovery and scan messages are
	// suppressed when nothing has changed.
	hasAnnounced bool

	// pendingUpdate holds a detected-but-not-yet-applied self-update.
	// Set by checkSelfUpdate, consumed by ApplyPendingUpdate or the
	// auto-apply timer. Protected by mu.
	pendingUpdate *CheckResult

	// autoApplyTimer fires after the grace period to auto-apply a pending
	// update if the user hasn't manually restarted. Nil when no update pending.
	autoApplyTimer *time.Timer

	// powerResumeCh receives a signal when the OS resumes from sleep/hibernate.
	// On Windows, this is wired to WM_POWERBROADCAST; nil on other platforms.
	powerResumeCh <-chan struct{}
}

type linkCodeResult struct {
	Code      string
	ExpiresAt time.Time
}

// New creates a Daemon with the given dependencies.
// A nil logger is replaced with a no-op logger.
func New(
	cfg Config,
	fsys FS,
	watcher Watcher,
	runner Runner,
	ws WSClient,
	plugins PluginManager,
	updater Updater,
	log *slog.Logger,
) *Daemon {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Daemon{
		cfg:                 cfg,
		fs:                  fsys,
		watcher:             watcher,
		runner:              runner,
		ws:                  ws,
		plugins:             plugins,
		updater:             updater,
		log:                 log,
		exitFunc:            os.Exit,
		restartFunc:         func(string, string) error { return nil },
		watchedDirs:         make(map[string]string),
		configDir:           defaultConfigDir(),
		pendingLinkCode:     make(chan linkCodeResult, 1),
		pluginUpdateResetCh: make(chan struct{}, 1),
		lastPushedSectionHashes: make(map[string]map[string][32]byte),
	}
}

// SetPluginReloadCh sets the channel that receives game IDs when a local
// plugin WASM file changes on disk. The daemon will reload the plugin and
// re-parse tracked saves for the game.
func (d *Daemon) SetPluginReloadCh(ch <-chan string) {
	d.pluginReloadCh = ch
}

// SetPowerResumeCh sets the channel that signals OS resume from sleep/hibernate.
// On resume, the daemon forces an immediate WebSocket reconnect.
func (d *Daemon) SetPowerResumeCh(ch <-chan struct{}) {
	d.powerResumeCh = ch
}

// SetRestartFunc sets the function called to restart the daemon after a
// self-update. On Windows this spawns a new process before exit; on Linux
// systemd handles restart so the default no-op suffices.
func (d *Daemon) SetRestartFunc(fn func(daemonPath, trayPath string) error) {
	d.restartFunc = fn
}

// PendingVersion returns the version string of a detected-but-not-applied
// update, or "" if none is pending.
func (d *Daemon) PendingVersion() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.pendingUpdate != nil && d.pendingUpdate.Daemon != nil {
		return d.pendingUpdate.Daemon.Version
	}
	return ""
}

// StorePendingUpdate stores a check result for deferred application.
// Exported for testability; normally called internally by checkSelfUpdate.
func (d *Daemon) StorePendingUpdate(result *CheckResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingUpdate = result
}

// ApplyPendingUpdate consumes and applies a stored pending update.
// If no update is pending, this is a no-op. Called by the local API
// restart handler or by the auto-apply timer.
func (d *Daemon) ApplyPendingUpdate(ctx context.Context) {
	d.mu.Lock()
	result := d.pendingUpdate
	d.pendingUpdate = nil
	if d.autoApplyTimer != nil {
		d.autoApplyTimer.Stop()
		d.autoApplyTimer = nil
	}
	d.mu.Unlock()

	if result == nil || result.Daemon == nil {
		return
	}

	d.applyDaemonUpdate(ctx, result)
}

// UpdatePlugins triggers an immediate plugin update check and returns
// the list of updated game IDs. It also resets the periodic update timer.
// Called by the local API endpoint handler.
func (d *Daemon) UpdatePlugins(ctx context.Context) ([]string, error) {
	if d.plugins == nil {
		return nil, fmt.Errorf("plugin manager not configured")
	}

	updated, err := d.plugins.CheckForUpdates(ctx)
	if err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
	}

	for _, gameID := range updated {
		d.sendMessage(ctx, &pb.Message{
			Payload: &pb.Message_PluginUpdated{PluginUpdated: &pb.PluginUpdated{
				GameId:  gameID,
				Version: "", // version is logged by pluginmgr; proto field is informational
			}},
		})

		// Re-parse tracked saves with the updated plugin.
		d.mu.RLock()
		gameCfg, ok := d.cfg.Games[gameID]
		d.mu.RUnlock()
		if ok {
			d.rescanQuiet(ctx, gameID, gameCfg)
		}
	}

	// Signal the event loop to reset the periodic timer (non-blocking).
	select {
	case d.pluginUpdateResetCh <- struct{}{}:
	default:
	}

	return updated, nil
}

// gameName returns the display name for a game, falling back to the raw gameID.
func (d *Daemon) gameName(ctx context.Context, gameID string) string {
	if d.plugins == nil {
		return gameID
	}
	manifests, err := d.plugins.Manifests(ctx)
	if err != nil {
		return gameID
	}
	if info, ok := manifests[gameID]; ok && info.Name != "" {
		return info.Name
	}
	return gameID
}

// SetLinkCallbacks registers callbacks for link state changes.
// Must be called before Run.
func (d *Daemon) SetLinkCallbacks(cb LinkCallbacks) {
	d.linkCB = cb
}

// SetInitialLinkCode sets the initial link code from registration.
// Called by the boot flow for newly registered sources.
func (d *Daemon) SetInitialLinkCode(code string, expiresAt time.Time) {
	d.linkCode = code
	d.linkExpiry = expiresAt
}

// RequestUnlink sends UnlinkSource over WS and blocks until the server
// responds with a new link code. Used by the repair endpoint.
func (d *Daemon) RequestUnlink(ctx context.Context) (string, time.Time, error) {
	// Drain any stale result.
	select {
	case <-d.pendingLinkCode:
	default:
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_UnlinkSource{UnlinkSource: &pb.UnlinkSource{}}})

	select {
	case <-ctx.Done():
		return "", time.Time{}, fmt.Errorf("unlink: %w", ctx.Err())
	case result := <-d.pendingLinkCode:
		return result.Code, result.ExpiresAt, nil
	}
}

func (d *Daemon) loadCachedConfig(ctx context.Context) {
	if len(d.cfg.Games) > 0 {
		return
	}
	if cached := loadConfigCache(d.configDir); len(cached) > 0 {
		d.log.InfoContext(ctx, "loaded config from cache", slog.Int("game_count", len(cached)))
		d.cfg.Games = cached
	}
}

// Run connects to the server and enters the main event loop.
// It blocks until ctx is canceled.
func (d *Daemon) Run(ctx context.Context) (runErr error) {
	d.startTime = time.Now()
	d.loadCachedConfig(ctx)

	d.log.InfoContext(ctx, "daemon starting",
		slog.String("source_id", d.cfg.SourceID),
		slog.String("version", d.cfg.Version),
		slog.Int("game_count", len(d.cfg.Games)),
	)

	if err := d.ws.Connect(ctx); err != nil {
		d.log.ErrorContext(ctx, "websocket connect failed", slog.String("error", err.Error()))
		return fmt.Errorf("ws connect: %w", err)
	}
	d.log.InfoContext(ctx, "websocket connected", slog.String("server_url", d.cfg.ServerURL))
	defer func() {
		if closeErr := d.ws.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("ws close: %w", closeErr)
		}
	}()

	d.announceOnline(ctx)
	return d.eventLoop(ctx)
}

func (d *Daemon) eventLoop(ctx context.Context) error {
	// Always create the plugin update ticker — the reset channel may fire
	// even when plugins are nil (the handler guards against that).
	updateTicker := time.NewTicker(pluginUpdateInterval)
	defer updateTicker.Stop()
	var updateCh <-chan time.Time
	if d.plugins != nil {
		updateCh = updateTicker.C
	}

	var selfUpdateTicker *time.Ticker
	var selfUpdateCh <-chan time.Time
	if d.updater != nil {
		selfUpdateTicker = time.NewTicker(selfUpdateInterval)
		selfUpdateCh = selfUpdateTicker.C
		defer selfUpdateTicker.Stop()
	}

	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.sendShutdown(ctx)
			return nil
		case ev := <-d.watcher.Events():
			d.handleFileEvent(ctx, ev)
		case msg := <-d.ws.Messages():
			d.handleCommand(ctx, msg)
		case gameID := <-d.pluginReloadCh:
			d.handlePluginReload(ctx, gameID)
		case <-updateCh:
			d.checkPluginUpdates(ctx)
		case <-selfUpdateCh:
			d.checkSelfUpdate(ctx)
		case <-heartbeatTicker.C:
			d.sendHeartbeat(ctx)
		case <-d.pluginUpdateResetCh:
			updateTicker.Reset(pluginUpdateInterval)
		case <-d.ws.Reconnected():
			d.log.InfoContext(ctx, "websocket reconnected, re-announcing")
			d.announceOnline(ctx)
		case <-d.powerResumeCh:
			d.log.InfoContext(ctx, "power resume detected, forcing websocket reconnect")
			d.ws.ForceReconnect()
		}
	}
}

// announceOnline sends the sourceOnline event and full game state.
// Called on initial connect and after each reconnect.
func (d *Daemon) announceOnline(ctx context.Context) {
	reconnect := d.hasAnnounced

	hostname, err := os.Hostname()
	if err != nil {
		d.log.WarnContext(ctx, "failed to get hostname", slog.String("error", err.Error()))
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOnline{SourceOnline: &pb.SourceOnline{
		Version:   d.cfg.Version,
		Platform:  runtime.GOOS + "-" + runtime.GOARCH,
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Hostname:  hostname,
		Device:    DetectDevice(),
		Timestamp: timestamppb.Now(),
	}}})

	if !reconnect {
		d.discoverGames(ctx)
	}

	for gameID, gameCfg := range d.cfg.Games {
		if !gameCfg.Enabled {
			d.log.DebugContext(ctx, "skipping disabled game", slog.String("game_id", gameID))
			continue
		}
		if !reconnect {
			d.log.InfoContext(ctx, "initializing game",
				slog.String("game", d.gameName(ctx, gameID)),
				slog.String("game_id", gameID),
				slog.String("save_path", gameCfg.SavePath),
			)
		}
		if !d.ensurePluginReady(ctx, gameID) {
			continue
		}
		d.scanGame(ctx, gameID, gameCfg, reconnect)
	}

	d.hasAnnounced = true
}

// autoApplyGracePeriod is how long after detecting an update the daemon waits
// before auto-applying. Gives the tray time to show the update badge.
// Variable (not const) so tests can shorten it.
var autoApplyGracePeriod = 15 * time.Minute //nolint:gochecknoglobals // test injection point

func (d *Daemon) checkSelfUpdate(ctx context.Context) {
	if d.updater == nil {
		return
	}
	result, err := d.updater.Check(ctx, d.cfg.Version, runtime.GOOS+"-"+runtime.GOARCH)
	if err != nil {
		return
	}
	if result == nil || result.Daemon == nil {
		return
	}
	d.log.InfoContext(ctx, "daemon update available", slog.String("new_version", result.Daemon.Version))

	d.mu.Lock()
	d.pendingUpdate = result
	// Cancel any previous auto-apply timer before starting a new one.
	if d.autoApplyTimer != nil {
		d.autoApplyTimer.Stop()
	}
	d.autoApplyTimer = time.AfterFunc(autoApplyGracePeriod, func() {
		if ctx.Err() != nil {
			return
		}
		d.ApplyPendingUpdate(ctx)
	})
	d.mu.Unlock()
}

func (d *Daemon) applyDaemonUpdate(ctx context.Context, result *CheckResult) {
	if d.updater == nil || result.Daemon == nil {
		return
	}
	d.sendMessage(
		ctx,
		&pb.Message{Payload: &pb.Message_SourceUpdateStarted{SourceUpdateStarted: &pb.SourceUpdateStarted{
			Version: result.Daemon.Version,
		}}},
	)
	if err := d.updater.Apply(ctx, result.Daemon, d.cfg.BinaryPath); err != nil {
		d.sendMessage(
			ctx,
			&pb.Message{Payload: &pb.Message_SourceUpdateFailed{SourceUpdateFailed: &pb.SourceUpdateFailed{
				Version: result.Daemon.Version,
				Message: err.Error(),
			}}},
		)
		return
	}

	// Update tray binary (best-effort, don't block daemon update).
	if result.Tray != nil && d.cfg.TrayBinaryPath != "" {
		if trayErr := d.updater.Apply(ctx, result.Tray, d.cfg.TrayBinaryPath); trayErr != nil {
			d.log.WarnContext(ctx, "tray update failed", slog.String("error", trayErr.Error()))
		}
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOffline{SourceOffline: &pb.SourceOffline{
		Timestamp: timestamppb.Now(),
	}}})

	// On Windows, spawn the new binary before exiting.
	// On Linux, systemd Restart=always handles restart after exit.
	if restartErr := d.restartFunc(d.cfg.BinaryPath, d.cfg.TrayBinaryPath); restartErr != nil {
		d.log.ErrorContext(ctx, "restart failed", slog.String("error", restartErr.Error()))
	}

	d.exitFunc(0)
}

func (d *Daemon) checkPluginUpdates(ctx context.Context) {
	updated, err := d.plugins.CheckForUpdates(ctx)
	if err != nil {
		d.sendMessage(
			ctx,
			&pb.Message{
				Payload: &pb.Message_PluginUpdateCheckFailed{PluginUpdateCheckFailed: &pb.PluginUpdateCheckFailed{
					Message: err.Error(),
				}},
			},
		)
		return
	}
	for _, gameID := range updated {
		d.log.InfoContext(
			ctx,
			"plugin updated",
			slog.String("game", d.gameName(ctx, gameID)),
			slog.String("game_id", gameID),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginUpdated{PluginUpdated: &pb.PluginUpdated{
			GameId: gameID,
		}}})
	}
}

// handlePluginReload is called when the PluginWatcher detects a local WASM
// file change. It reloads the plugin via EnsurePlugin (which re-reads from
// the local dir) and re-parses all tracked saves for the game.
func (d *Daemon) handlePluginReload(ctx context.Context, gameID string) {
	d.log.InfoContext(ctx, "local plugin changed, reloading",
		slog.String("game_id", gameID),
	)

	if !d.ensurePluginReady(ctx, gameID) {
		return
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginUpdated{PluginUpdated: &pb.PluginUpdated{
		GameId: gameID,
	}}})

	// Re-parse tracked saves for this game.
	d.mu.RLock()
	gameCfg, ok := d.cfg.Games[gameID]
	d.mu.RUnlock()
	if !ok {
		return
	}

	d.rescanQuiet(ctx, gameID, gameCfg)
}

// ensurePluginReady downloads/verifies the plugin for gameID if a
// PluginManager is configured. Returns true if the plugin is ready.
func (d *Daemon) ensurePluginReady(
	ctx context.Context, gameID string,
) bool {
	if d.plugins == nil {
		return true
	}
	d.log.DebugContext(ctx, "ensuring plugin ready", slog.String("game_id", gameID))
	if ensureErr := d.plugins.EnsurePlugin(ctx, gameID); ensureErr != nil {
		d.log.ErrorContext(
			ctx,
			"plugin download failed",
			slog.String("game_id", gameID),
			slog.String("error", ensureErr.Error()),
		)
		d.sendMessage(
			ctx,
			&pb.Message{Payload: &pb.Message_PluginDownloadFailed{PluginDownloadFailed: &pb.PluginDownloadFailed{
				GameId:  gameID,
				Message: ensureErr.Error(),
			}}},
		)
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
		d.log.WarnContext(ctx, "failed to fetch plugin manifests", slog.String("error", err.Error()))
		return
	}

	var discovered []DiscoveredGame
	for gameID, info := range manifests {
		pathTemplate, ok := info.DefaultPaths[runtime.GOOS]
		if !ok || pathTemplate == "" {
			continue
		}

		expanded := expandPath(pathTemplate)
		dirs := resolveGlob(d.fs, expanded, info.ExcludeDirs)

		// Check that at least one resolved path is a valid directory.
		anyValid := false
		totalMatching := 0
		for _, dir := range dirs {
			stat, statErr := d.fs.Stat(dir)
			if statErr != nil || !stat.IsDir() {
				continue
			}
			anyValid = true
			entries, readErr := d.fs.ReadDir(dir)
			if readErr != nil {
				continue
			}
			totalMatching += len(d.filterSaveFiles(entries, info.FileExtensions, info.FilePatterns, nil))
		}
		if !anyValid {
			continue
		}

		d.log.InfoContext(ctx, "game discovered",
			slog.String("game_id", gameID),
			slog.String("name", info.Name),
			slog.String("path", expanded),
			slog.Int("file_count", totalMatching),
		)
		discovered = append(discovered, DiscoveredGame{
			GameID:         gameID,
			Name:           info.Name,
			Path:           expanded,
			FileCount:      totalMatching,
			FileExtensions: info.FileExtensions,
			FilePatterns:   info.FilePatterns,
			ExcludeDirs:    info.ExcludeDirs,
		})
	}

	pbGames := make([]*pb.DiscoveredGame, len(discovered))
	for i, game := range discovered {
		pbGames[i] = &pb.DiscoveredGame{
			GameId:         game.GameID,
			Name:           game.Name,
			Path:           game.Path,
			FileCount:      int32(game.FileCount), // #nosec G115 -- bounded by filesystem limits
			FileExtensions: game.FileExtensions,
			FilePatterns:   game.FilePatterns,
			ExcludeDirs:    game.ExcludeDirs,
		}
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GamesDiscovered{GamesDiscovered: &pb.GamesDiscovered{
		Games: pbGames,
	}}})
}

// rescanQuiet re-parses files for an already-watched game without sending
// discovery/scan/watch messages. Returns true if handled (dir was already
// watched), false if the caller should fall through to a full scan.
func (d *Daemon) rescanQuiet(
	ctx context.Context, gameID string, cfg GameConfig,
) bool {
	dirs := resolveGlob(d.fs, cfg.SavePath, cfg.ExcludeDirs)

	// Check if any resolved path is already watched.
	d.mu.RLock()
	anyWatched := false
	for _, dir := range dirs {
		if _, ok := d.watchedDirs[dir]; ok {
			anyWatched = true
			break
		}
	}
	d.mu.RUnlock()

	if !anyWatched {
		return false
	}

	for _, dir := range dirs {
		entries, err := d.fs.ReadDir(dir)
		if err != nil {
			continue
		}
		matchingFiles := d.filterSaveFiles(entries, cfg.FileExtensions, cfg.FilePatterns, cfg.ExcludeSaves)
		for _, fileName := range matchingFiles {
			fullPath := filepath.Join(dir, fileName)
			d.parseAndPush(ctx, gameID, fullPath, fileName, nil, true)
		}
	}
	return true
}

func (d *Daemon) scanGame(
	ctx context.Context, gameID string, cfg GameConfig, quiet bool,
) {
	// On reconnect (quiet=true), skip straight to re-parsing files.
	// The hash cache in pushState handles dedup; discovery/scan/watch
	// messages are suppressed because we already sent them.
	if quiet && d.rescanQuiet(ctx, gameID, cfg) {
		return
	}

	displayName := d.gameName(ctx, gameID)
	dirs := resolveGlob(d.fs, cfg.SavePath, cfg.ExcludeDirs)

	d.log.InfoContext(
		ctx,
		"scanning game directory",
		slog.String("game", displayName),
		slog.String("game_id", gameID),
		slog.String("path", cfg.SavePath),
	)
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ScanStarted{ScanStarted: &pb.ScanStarted{
		GameId: gameID,
		Path:   cfg.SavePath,
	}}})

	allDirFiles, allMatchingFiles, validDirs := d.collectSaveFiles(
		dirs, cfg.FileExtensions, cfg.FilePatterns, cfg.ExcludeSaves,
	)

	if validDirs == 0 {
		d.log.WarnContext(
			ctx,
			"game directory not found",
			slog.String("game_id", gameID),
			slog.String("path", cfg.SavePath),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: dirs,
		}}})
		return
	}

	d.log.InfoContext(ctx, "save files found",
		slog.String("game", displayName),
		slog.String("game_id", gameID),
		slog.Int("count", len(allMatchingFiles)),
		slog.String("path", cfg.SavePath),
	)

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ScanCompleted{ScanCompleted: &pb.ScanCompleted{
		GameId:     gameID,
		Path:       cfg.SavePath,
		FilesFound: int32(len(allMatchingFiles)), // #nosec G115 -- bounded by filesystem limits
		FileNames:  allMatchingFiles,
	}}})

	if len(allMatchingFiles) == 0 {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: dirs,
		}}})
		return
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameDetected{GameDetected: &pb.GameDetected{
		GameId:    gameID,
		Path:      cfg.SavePath,
		SaveCount: int32(len(allMatchingFiles)), // #nosec G115 -- bounded by filesystem limits
	}}})

	// Watch each resolved directory that has matching files.
	for _, df := range allDirFiles {
		if watchErr := d.watcher.Add(df.dir); watchErr != nil {
			continue
		}
		d.mu.Lock()
		d.watchedDirs[df.dir] = gameID
		d.mu.Unlock()
	}

	d.log.InfoContext(
		ctx,
		"watching game",
		slog.String("game", displayName),
		slog.String("game_id", gameID),
		slog.String("path", cfg.SavePath),
		slog.Int("file_count", len(allMatchingFiles)),
	)
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_Watching{Watching: &pb.Watching{
		GameId:         gameID,
		Path:           cfg.SavePath,
		FilesMonitored: int32(len(allMatchingFiles)), // #nosec G115 -- bounded by filesystem limits
	}}})

	for _, df := range allDirFiles {
		for _, fileName := range df.files {
			fullPath := filepath.Join(df.dir, fileName)
			d.parseAndPush(ctx, gameID, fullPath, fileName, nil, false)
		}
	}
}

// dirFiles pairs a directory path with the save file names found inside it.
type dirFiles struct {
	dir   string
	files []string
}

// collectSaveFiles scans each directory for files matching the given extensions and patterns.
// Returns the per-directory results, a flat list of all matching file names,
// and the count of valid directories examined.
func (d *Daemon) collectSaveFiles(dirs, extensions, patterns, excludeSaves []string) ([]dirFiles, []string, int) {
	var result []dirFiles
	var allFiles []string
	validDirs := 0

	for _, dir := range dirs {
		info, err := d.fs.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		validDirs++

		entries, err := d.fs.ReadDir(dir)
		if err != nil {
			continue
		}

		matching := d.filterSaveFiles(entries, extensions, patterns, excludeSaves)
		if len(matching) > 0 {
			result = append(result, dirFiles{dir: dir, files: matching})
			allFiles = append(allFiles, matching...)
		}
	}

	return result, allFiles, validDirs
}

func (d *Daemon) handleFileEvent(ctx context.Context, ev FileEvent) {
	if ev.Op == FileRemove {
		return
	}

	dir := filepath.Dir(ev.Path)
	d.mu.RLock()
	gameID, ok := d.watchedDirs[dir]
	d.mu.RUnlock()
	if !ok {
		return
	}
	d.log.DebugContext(
		ctx,
		"file event",
		slog.String("game_id", gameID),
		slog.String("path", ev.Path),
		slog.Int("op", int(ev.Op)),
	)

	d.mu.RLock()
	gameCfg := d.cfg.Games[gameID]
	d.mu.RUnlock()
	fileName := filepath.Base(ev.Path)
	ext := filepath.Ext(fileName)
	if !matchesExtension(ext, gameCfg.FileExtensions) {
		return
	}
	if len(gameCfg.FilePatterns) > 0 && !matchesPattern(fileName, gameCfg.FilePatterns) {
		return
	}
	if isExcludedSave(fileName, gameCfg.ExcludeSaves) {
		return
	}

	d.parseAndPush(ctx, gameID, ev.Path, fileName, ev.Data, false)
}

// parseAndPush reads the save file, runs the plugin, and pushes the result.
// When preloadedData is non-nil (e.g. from the watcher's SHA-256 read), it is
// used directly, avoiding a redundant filesystem read.
// When quiet is true (reconnect with unchanged files), ParseStarted and
// ParseCompleted messages are suppressed.
func (d *Daemon) parseAndPush(
	ctx context.Context, gameID, fullPath, fileName string,
	preloadedData []byte, quiet bool,
) {
	d.log.DebugContext(ctx, "parsing save file", slog.String("game_id", gameID), slog.String("file_name", fileName))
	if !quiet {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseStarted{ParseStarted: &pb.ParseStarted{
			GameId:   gameID,
			FileName: fileName,
		}}})
	}

	saveBytes := preloadedData
	if saveBytes == nil {
		var err error
		saveBytes, err = d.fs.ReadFile(fullPath)
		if err != nil {
			d.log.ErrorContext(
				ctx,
				"failed to read save file",
				slog.String("game_id", gameID),
				slog.String("file_name", fileName),
				slog.String("error", err.Error()),
			)
			d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseFailed{ParseFailed: &pb.ParseFailed{
				GameId:    gameID,
				FileName:  fileName,
				ErrorType: pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR,
				Message:   fmt.Sprintf("read file: %v", err),
			}}})
			return
		}
	}

	onStatus := func(message string) {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginStatus{PluginStatus: &pb.PluginStatus{
			GameId:   gameID,
			FileName: fileName,
			Message:  message,
		}}})
	}

	state, err := d.runner.Run(ctx, gameID, fileName, saveBytes, onStatus)
	if err != nil {
		errorType := "PARSE_ERROR_TYPE_PARSE_ERROR"
		if pluginErr, ok := errors.AsType[*PluginError](err); ok {
			errorType = pluginErr.Type
		}
		d.log.ErrorContext(
			ctx,
			"plugin parse failed",
			slog.String("game_id", gameID),
			slog.String("file_name", fileName),
			slog.String("error_type", errorType),
			slog.String("error", err.Error()),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseFailed{ParseFailed: &pb.ParseFailed{
			GameId:    gameID,
			FileName:  fileName,
			ErrorType: toParseErrorType(errorType),
			Message:   err.Error(),
		}}})
		return
	}

	if !quiet {
		d.log.InfoContext(
			ctx,
			"parse completed",
			slog.String("game", d.gameName(ctx, gameID)),
			slog.String("game_id", gameID),
			slog.String("file_name", fileName),
			slog.String("summary", state.Summary),
			slog.Int("sections_count", len(state.Sections)),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseCompleted{ParseCompleted: &pb.ParseCompleted{
			GameId:        gameID,
			FileName:      fileName,
			Identity:      toProtoIdentity(state.Identity),
			Summary:       state.Summary,
			SectionsCount: int32(len(state.Sections)), // #nosec G115 -- bounded by game limits
		}}})
	}

	d.pushState(ctx, gameID, fullPath, state)
}

func (d *Daemon) pushState(
	ctx context.Context, gameID, filePath string, state *GameState,
) {
	sections := make([]*pb.GameSection, 0, len(state.Sections))
	for name, section := range state.Sections {
		if section.Data.Kind() != '{' {
			d.log.ErrorContext(ctx, "section data is not a JSON object, skipping",
				slog.String("game_id", gameID),
				slog.String("section", name),
				slog.String("kind", string(section.Data.Kind())),
			)
			continue
		}

		var dataMap map[string]any
		if err := json.Unmarshal(section.Data, &dataMap); err != nil {
			d.log.ErrorContext(ctx, "failed to unmarshal section data",
				slog.String("game_id", gameID),
				slog.String("section", name),
				slog.String("error", err.Error()),
			)
			continue
		}

		dataStruct, err := structpb.NewStruct(dataMap)
		if err != nil {
			d.log.ErrorContext(ctx, "failed to convert section data to proto struct",
				slog.String("game_id", gameID),
				slog.String("section", name),
				slog.String("error", err.Error()),
			)
			continue
		}

		sections = append(sections, &pb.GameSection{
			Name:        name,
			Description: section.Description,
			Data:        dataStruct,
		})
	}

	// Sort sections by name for deterministic hashing (map iteration order is random).
	slices.SortFunc(sections, func(a, b *pb.GameSection) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Hash each section individually and filter to only changed sections.
	opts := proto.MarshalOptions{Deterministic: true}
	prevHashes := d.lastPushedSectionHashes[filePath]
	newHashes := make(map[string][32]byte, len(sections))
	var changed []*pb.GameSection

	for _, s := range sections {
		sectionBytes, err := opts.Marshal(s)
		if err != nil {
			d.log.ErrorContext(ctx, "failed to marshal section for hashing",
				slog.String("game_id", gameID),
				slog.String("section", s.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		h := sha256.Sum256(sectionBytes)
		newHashes[s.Name] = h
		if prev, ok := prevHashes[s.Name]; !ok || prev != h {
			changed = append(changed, s)
		}
	}

	if len(changed) == 0 {
		d.log.DebugContext(ctx, "save data unchanged, skipping push",
			slog.String("game_id", gameID),
			slog.String("file_path", filePath),
		)
		return
	}

	d.log.InfoContext(ctx, "pushing save data",
		slog.String("game", d.gameName(ctx, gameID)),
		slog.String("game_id", gameID),
		slog.String("summary", state.Summary),
		slog.Int("sections_changed", len(changed)),
		slog.Int("sections_total", len(sections)),
	)

	pushSave := &pb.PushSave{
		Identity: toProtoIdentity(state.Identity),
		Summary:  state.Summary,
		Sections: changed,
		GameId:   gameID,
		ParsedAt: timestamppb.Now(),
	}
	msg := &pb.Message{Payload: &pb.Message_PushSave{PushSave: pushSave}}
	data, err := opts.Marshal(msg)
	if err != nil {
		d.log.ErrorContext(ctx, "failed to marshal PushSave message",
			slog.String("game_id", gameID),
			slog.String("error", err.Error()),
		)
		return
	}
	if sendErr := d.ws.Send(data); sendErr != nil {
		d.log.WarnContext(ctx, "failed to send message", slog.String("error", sendErr.Error()))
		return
	}
	d.lastPushedSectionHashes[filePath] = newHashes
}

func (d *Daemon) handleCommand(ctx context.Context, data []byte) {
	var msg pb.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		d.log.WarnContext(ctx, "failed to unmarshal command", slog.String("error", err.Error()))
		return
	}

	switch cmd := msg.Payload.(type) {
	case *pb.Message_ConfigUpdate:
		d.handleConfigUpdate(ctx, cmd.ConfigUpdate)
	case *pb.Message_RescanGame:
		d.mu.RLock()
		gameCfg, ok := d.cfg.Games[cmd.RescanGame.GameId]
		d.mu.RUnlock()
		if ok {
			d.scanGame(ctx, cmd.RescanGame.GameId, gameCfg, false)
		}
	case *pb.Message_TestPath:
		d.handleTestPath(ctx, cmd.TestPath.GameId, cmd.TestPath.Path)
	case *pb.Message_DiscoverGames:
		d.discoverGames(ctx)
	case *pb.Message_PushSaveResult:
		d.handlePushSaveResult(ctx, cmd.PushSaveResult)
	case *pb.Message_SourceUpdateAvailable:
		// Server-pushed updates only contain daemon info.
		// The tray will update on the next poll-based manifest check.
		info := &UpdateInfo{
			Version:      cmd.SourceUpdateAvailable.Version,
			URL:          cmd.SourceUpdateAvailable.Url,
			SignatureURL: cmd.SourceUpdateAvailable.SignatureUrl,
			SHA256:       cmd.SourceUpdateAvailable.Sha256,
		}
		d.applyDaemonUpdate(ctx, &CheckResult{Daemon: info})
	case *pb.Message_PluginAvailable:
		d.handlePluginAvailable(ctx, cmd.PluginAvailable)
	case *pb.Message_SourceLinked:
		d.handleSourceLinked(ctx)
	case *pb.Message_RefreshLinkCodeResult:
		d.handleRefreshLinkCodeResult(ctx, cmd.RefreshLinkCodeResult)
	}
}

func (d *Daemon) handlePluginAvailable(ctx context.Context, msg *pb.PluginAvailable) {
	if d.plugins == nil {
		d.log.WarnContext(ctx, "received PluginAvailable but no plugin manager configured",
			slog.String("game_id", msg.GameId))
		return
	}

	d.log.InfoContext(ctx, "plugin update available",
		slog.String("game_id", msg.GameId),
		slog.String("version", msg.Version),
	)

	if err := d.plugins.EnsurePlugin(ctx, msg.GameId); err != nil {
		d.log.ErrorContext(ctx, "failed to download plugin",
			slog.String("game_id", msg.GameId),
			slog.String("error", err.Error()),
		)
		d.sendMessage(ctx, &pb.Message{
			Payload: &pb.Message_PluginDownloadFailed{PluginDownloadFailed: &pb.PluginDownloadFailed{
				GameId:  msg.GameId,
				Message: "plugin download failed",
			}},
		})
		return
	}

	d.sendMessage(ctx, &pb.Message{
		Payload: &pb.Message_PluginUpdated{PluginUpdated: &pb.PluginUpdated{
			GameId:  msg.GameId,
			Version: msg.Version,
		}},
	})

	// Reset the periodic update timer.
	select {
	case d.pluginUpdateResetCh <- struct{}{}:
	default:
	}
}

func (d *Daemon) handleSourceLinked(ctx context.Context) {
	d.mu.Lock()
	d.linked = true
	d.linkCode = ""
	d.linkExpiry = time.Time{}
	d.mu.Unlock()

	d.log.InfoContext(ctx, "source linked to user")
	if d.linkCB.OnLinked != nil {
		d.linkCB.OnLinked()
	}
}

func (d *Daemon) handleRefreshLinkCodeResult(ctx context.Context, result *pb.RefreshLinkCodeResult) {
	var expiresAt time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt.AsTime()
	}

	d.mu.Lock()
	d.linkCode = result.LinkCode
	d.linkExpiry = expiresAt
	d.mu.Unlock()

	d.log.InfoContext(ctx, "link code received",
		slog.Time("expires_at", expiresAt),
	)

	if d.linkCB.OnLinkCode != nil {
		d.linkCB.OnLinkCode(result.LinkCode, expiresAt)
	}

	// Deliver to any synchronous waiter (e.g. repair endpoint).
	// Non-blocking: pendingLinkCode is buffered(1) with a single consumer
	// (RequestUnlink). If no waiter exists, the result is silently dropped.
	select {
	case d.pendingLinkCode <- linkCodeResult{Code: result.LinkCode, ExpiresAt: expiresAt}:
	default:
	}
}

func (d *Daemon) sendShutdown(ctx context.Context) {
	d.log.InfoContext(ctx, "daemon shutting down")
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOffline{SourceOffline: &pb.SourceOffline{
		Timestamp: timestamppb.Now(),
	}}})
}

func (d *Daemon) sendHeartbeat(ctx context.Context) {
	d.sendMessage(
		ctx,
		&pb.Message{Payload: &pb.Message_SourceHeartbeat{SourceHeartbeat: &pb.SourceHeartbeat{}}},
	)
	d.maybeRefreshLinkCode(ctx)
}

// refreshThreshold is how close to expiry we refresh the link code.
const refreshThreshold = 2 * time.Minute

func (d *Daemon) maybeRefreshLinkCode(ctx context.Context) {
	d.mu.RLock()
	linked := d.linked
	expiry := d.linkExpiry
	d.mu.RUnlock()

	if linked || expiry.IsZero() {
		return
	}

	if time.Until(expiry) < refreshThreshold {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_RefreshLinkCode{RefreshLinkCode: &pb.RefreshLinkCode{}}})
	}
}

// configGameResult is the per-game result of applying a ConfigUpdate.
type configGameResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	ResolvedPath string `json:"resolvedPath"`
}

// buildGameResult checks if a resolved path is a valid directory.
func (d *Daemon) buildGameResult(resolvedPath string, excludeDirs []string) configGameResult {
	dirs := resolveGlob(d.fs, resolvedPath, excludeDirs)
	for _, dir := range dirs {
		info, err := d.fs.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		// At least one resolved directory exists.
		return configGameResult{Success: true, ResolvedPath: resolvedPath}
	}
	return configGameResult{Error: fmt.Sprintf("path not found: %s", resolvedPath), ResolvedPath: resolvedPath}
}

func (d *Daemon) handlePushSaveResult(ctx context.Context, result *pb.PushSaveResult) {
	if result.Error == pb.PushSaveError_PUSH_SAVE_ERROR_GAME_REMOVED {
		gameID := result.GameId
		d.mu.Lock()
		gameCfg, existed := d.cfg.Games[gameID]
		if existed {
			delete(d.cfg.Games, gameID)
		}
		d.mu.Unlock()
		if existed {
			d.log.InfoContext(ctx, "game removed by server",
				slog.String("game", d.gameName(ctx, gameID)),
				slog.String("game_id", gameID),
			)
			d.unwatchGame(ctx, gameCfg.SavePath)
		}
		return
	}
	if result.Error == pb.PushSaveError_PUSH_SAVE_ERROR_SAVE_REMOVED {
		d.log.WarnContext(ctx, "save removed by server",
			slog.String("game_id", result.GameId),
			slog.String("save_uuid", result.SaveUuid),
		)
		return
	}
	d.log.InfoContext(ctx, "push acknowledged",
		slog.String("save_uuid", result.SaveUuid),
	)
}

func (d *Daemon) handleConfigUpdate(
	ctx context.Context, update *pb.ConfigUpdate,
) {
	d.log.InfoContext(ctx, "config update received", slog.Int("game_count", len(update.Games)))

	d.removeStaleGames(ctx, update.Games)

	results := make(map[string]configGameResult, len(update.Games))

	for gameID, newGame := range update.Games {
		resolvedPath := expandPath(newGame.SavePath)
		gameCfg := GameConfig{
			SavePath:       resolvedPath,
			Enabled:        newGame.Enabled,
			FileExtensions: newGame.FileExtensions,
			FilePatterns:   newGame.FilePatterns,
			ExcludeDirs:    newGame.ExcludeDirs,
			ExcludeSaves:   newGame.ExcludeSaves,
		}

		d.mu.Lock()
		oldCfg, existed := d.cfg.Games[gameID]
		d.cfg.Games[gameID] = gameCfg
		d.mu.Unlock()

		switch {
		case !newGame.Enabled:
			d.log.InfoContext(
				ctx,
				"game disabled",
				slog.String("game", d.gameName(ctx, gameID)),
				slog.String("game_id", gameID),
			)
			if existed {
				d.unwatchGame(ctx, oldCfg.SavePath)
			}
			results[gameID] = configGameResult{Success: true}
		case !existed || !oldCfg.Enabled:
			d.log.InfoContext(
				ctx,
				"new game configured",
				slog.String("game", d.gameName(ctx, gameID)),
				slog.String("game_id", gameID),
				slog.String("save_path", resolvedPath),
				slog.Bool("enabled", newGame.Enabled),
			)
			if !d.ensurePluginReady(ctx, gameID) {
				d.mu.Lock()
				delete(d.cfg.Games, gameID)
				d.mu.Unlock()
				results[gameID] = configGameResult{Error: "plugin download failed", ResolvedPath: resolvedPath}
				continue
			}
			d.scanGame(ctx, gameID, gameCfg, false)
			results[gameID] = d.buildGameResult(resolvedPath, gameCfg.ExcludeDirs)
		case oldCfg.SavePath != resolvedPath:
			d.log.InfoContext(
				ctx,
				"game path changed",
				slog.String("game", d.gameName(ctx, gameID)),
				slog.String("game_id", gameID),
				slog.String("old_path", oldCfg.SavePath),
				slog.String("new_path", resolvedPath),
			)
			d.unwatchGame(ctx, oldCfg.SavePath)
			if !d.ensurePluginReady(ctx, gameID) {
				d.mu.Lock()
				delete(d.cfg.Games, gameID)
				d.mu.Unlock()
				results[gameID] = configGameResult{Error: "plugin download failed", ResolvedPath: resolvedPath}
				continue
			}
			d.scanGame(ctx, gameID, gameCfg, false)
			results[gameID] = d.buildGameResult(resolvedPath, gameCfg.ExcludeDirs)
		default:
			// No change needed — game already configured with same path.
			results[gameID] = configGameResult{Success: true, ResolvedPath: resolvedPath}
		}
	}

	pbResults := make(map[string]*pb.GameConfigResult, len(results))
	for gameID, r := range results {
		pbResults[gameID] = &pb.GameConfigResult{
			Success:      r.Success,
			Error:        r.Error,
			ResolvedPath: r.ResolvedPath,
		}
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ConfigResult{ConfigResult: &pb.ConfigResult{
		Results: pbResults,
	}}})

	d.mu.RLock()
	games := make(map[string]GameConfig, len(d.cfg.Games))
	maps.Copy(games, d.cfg.Games)
	d.mu.RUnlock()
	if err := saveConfigCache(d.configDir, games); err != nil {
		d.log.WarnContext(ctx, "failed to save config cache", slog.String("error", err.Error()))
	}
}

func (d *Daemon) removeStaleGames(ctx context.Context, newGames map[string]*pb.GameConfig) {
	newGameIDs := make(map[string]bool, len(newGames))
	for gameID := range newGames {
		newGameIDs[gameID] = true
	}

	d.mu.Lock()
	var stale []struct {
		gameID   string
		savePath string
	}
	for gameID, oldCfg := range d.cfg.Games {
		if !newGameIDs[gameID] {
			stale = append(stale, struct {
				gameID   string
				savePath string
			}{gameID, oldCfg.SavePath})
		}
	}
	for _, s := range stale {
		delete(d.cfg.Games, s.gameID)
	}
	d.mu.Unlock()

	for _, s := range stale {
		d.unwatchGame(ctx, s.savePath)
	}
}

func (d *Daemon) unwatchGame(ctx context.Context, savePath string) {
	dirs := resolveGlob(d.fs, savePath, nil)

	d.mu.Lock()
	var toRemove []string
	for _, dir := range dirs {
		if _, ok := d.watchedDirs[dir]; ok {
			delete(d.watchedDirs, dir)
			toRemove = append(toRemove, dir)
		}
	}
	d.mu.Unlock()

	for _, dir := range toRemove {
		if removeErr := d.watcher.Remove(dir); removeErr != nil {
			d.log.DebugContext(
				ctx,
				"watcher remove failed",
				slog.String("save_path", dir),
				slog.String("error", removeErr.Error()),
			)
		}
	}
}

func (d *Daemon) handleTestPath(ctx context.Context, gameID, path string) {
	d.mu.RLock()
	gameCfg := d.cfg.Games[gameID]
	d.mu.RUnlock()

	dirs := resolveGlob(d.fs, path, gameCfg.ExcludeDirs)
	var allFileNames []string
	for _, dir := range dirs {
		info, err := d.fs.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		entries, err := d.fs.ReadDir(dir)
		if err != nil {
			continue
		}
		allFileNames = append(
			allFileNames,
			d.filterSaveFiles(entries, gameCfg.FileExtensions, gameCfg.FilePatterns, gameCfg.ExcludeSaves)...,
		)
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_TestPathResult{TestPathResult: &pb.TestPathResult{
		GameId:     gameID,
		Path:       path,
		Valid:      len(allFileNames) > 0,
		FilesFound: int32(len(allFileNames)), // #nosec G115 -- bounded by filesystem limits
		FileNames:  allFileNames,
	}}})
}

func (d *Daemon) filterSaveFiles(
	entries []fs.DirEntry, extensions, patterns, excludeSaves []string,
) []string {
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !matchesExtension(ext, extensions) {
			continue
		}
		if len(patterns) > 0 && !matchesPattern(name, patterns) {
			continue
		}
		if isExcludedSave(name, excludeSaves) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// matchesPattern checks if filename matches any of the given glob patterns.
func matchesPattern(name string, patterns []string) bool {
	for _, pat := range patterns {
		matched, err := filepath.Match(pat, name)
		if err != nil {
			continue // malformed pattern — skip
		}
		if matched {
			return true
		}
	}
	return false
}

func matchesExtension(ext string, extensions []string) bool {
	for _, want := range extensions {
		if strings.EqualFold(ext, want) {
			return true
		}
	}
	return false
}

func (d *Daemon) sendMessage(ctx context.Context, msg *pb.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		d.log.ErrorContext(ctx, "failed to marshal proto message", slog.String("error", err.Error()))
		return
	}
	if sendErr := d.ws.Send(data); sendErr != nil {
		d.log.WarnContext(ctx, "failed to send message", slog.String("error", sendErr.Error()))
	}
}

// toParseErrorType converts a string error type to the proto enum.
func toParseErrorType(s string) pb.ParseErrorType {
	if v, ok := pb.ParseErrorType_value[s]; ok {
		return pb.ParseErrorType(v)
	}
	return pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR
}

// toProtoIdentity converts a daemon Identity to a proto SaveIdentity.
func toProtoIdentity(id Identity) *pb.SaveIdentity {
	si := &pb.SaveIdentity{Name: id.SaveName}
	if len(id.Extra) > 0 {
		extra, err := structpb.NewStruct(id.Extra)
		if err == nil {
			si.Extra = extra
		}
	}
	return si
}
