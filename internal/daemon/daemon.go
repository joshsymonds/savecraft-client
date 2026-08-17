// Package daemon coordinates file watching, plugin execution, and server communication.
package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft-client/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft-client/internal/proto/savecraft/v1"
)

const pluginUpdateInterval = 24 * time.Hour
const selfUpdateInterval = 6 * time.Hour
const heartbeatInterval = 30 * time.Second

// unknownSaveName is the identity substituted by resolveIdentity when a
// parse yields an empty saveName ("identity unknown") and no prior name has
// been seen for the file path.
const unknownSaveName = "Unknown Player"

const pluginDownloadFailedMessage = "plugin download failed"

// unitDirectory is the PluginInfo.Unit value selecting directory-unit save
// semantics: each resolved default_paths directory is one save, whose
// Members are tar-archived together and dispatched as a single unit. Any
// other value (including the empty default) is the existing file-unit
// behavior — one save per matching file.
const unitDirectory = "directory"

// dirUnitTarFileMode is the POSIX file mode recorded for every member entry
// in a directory-unit's tar archive (see buildDirectoryUnitTar). Members are
// read-only save data; the mode is metadata the plugin never inspects.
const dirUnitTarFileMode = 0o644

const (
	pluginErrorTypeUnsupportedVersion = "unsupported_version"
	pluginErrorTypeCorruptFile        = "corrupt_file"
	pluginErrorTypeParseError         = "parse_error"
	// PluginErrorTypeResourceLimit is the plugin errorType for a well-formed
	// save that exceeds a size or memory cap (not corruption). Exported because
	// the runner synthesizes it when a plugin dies of an out-of-memory trap.
	PluginErrorTypeResourceLimit = "resource_limit"
)

// --- Domain types ---

// GameState is the structured output from parsing a save file.
type GameState struct {
	Identity Identity           `json:"identity"`
	Summary  string             `json:"summary"`
	Sections map[string]Section `json:"sections"`
}

// Identity identifies a specific save within a game.
type Identity struct {
	SaveName    string         `json:"saveName,omitempty"`
	GameID      string         `json:"gameId"`
	Extra       map[string]any `json:"extra,omitempty"`
	DisplayName string         `json:"displayName,omitempty"`
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
	// AddDirectoryUnit recursively watches root and every non-excluded
	// subdirectory beneath it, for directory-unit save plugins. Unlike Add,
	// every member change beneath root is coalesced into a single FileEvent
	// addressed to root itself, once the directory quiesces.
	AddDirectoryUnit(root string, excludeDirs []string) error
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
	Start(ctx context.Context)
	Send(msg []byte) error
	Messages() <-chan []byte
	Connected() <-chan struct{}
	ForceReconnect()
	Close() error
	IsConnected() bool
}

// FS abstracts filesystem operations for testability.
type FS interface {
	Stat(path string) (fs.FileInfo, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	ReadFile(path string) ([]byte, error)
	// EvalSymlinks resolves symlinks in path (like filepath.EvalSymlinks).
	// Used by the save-path allowlist so a symlinked save dir cannot escape
	// the allowed roots.
	EvalSymlinks(path string) (string, error)
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
	mu                sync.RWMutex
	pluginGenerations map[string]uint64

	// Maps watched directory -> game ID.
	watchedDirs map[string]string

	// configDir is the directory for persisting config cache.
	// Defaults to os.UserConfigDir()/savecraft; empty disables caching.
	configDir string

	// allowedSaveRoots is the locally-computed allowlist of directory roots
	// the daemon may read/enumerate for saves. The server is trusted for
	// config, but this is defense-in-depth: a compromised server cannot turn
	// TestPath/SavePath into an arbitrary local-file read or home-dir
	// enumeration primitive (finding 4.3 / R12). Computed at New() from the
	// user's home plus a small per-OS set of known game/save roots; the
	// server may select paths WITHIN these roots, never outside them.
	// Overridable in tests.
	allowedSaveRoots []string

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
	// successfully pushed GameSection proto bytes, keyed by file path. Each
	// entry also records the save identity (saveName) the hashes belong to.
	// On re-parse, only sections whose hash changed are included in the
	// PushSave. If no sections changed, the push is skipped entirely.
	//
	// The cache is scoped to save identity, not just file path, because the
	// server routes pushes by (gameID, saveName) rather than by file path.
	// A single file can parse to different save identities across
	// consecutive reads (e.g. MTGA log rotation, where a boot-only log
	// parses to the fallback name "Unknown Player" and the next session log
	// parses back to the real player name). If the cache were keyed by file
	// path alone, a delta would be computed against a different save's
	// content, so the push would carry only sections that happen to differ
	// between the two saves — the server would then create/update a save
	// from that partial payload, permanently missing every section whose
	// hash happened to match. When the current parse's saveName differs
	// from the cached one (or there is no cached entry), the cache is
	// treated as cold: the push includes all sections, and the cache is
	// re-seeded under the new identity.
	lastPushedSectionHashes map[string]*sectionHashCache

	// lastKnownSaveNames caches the most recently observed non-empty save
	// identity name for each file path, keyed by file path. The plugin
	// contract treats an empty identity.saveName as "identity unknown" (for
	// example a boot-only MTGA Player.log with no login event yet), not as
	// "no identity" — every real save, including game-scoped saves like a
	// D2R shared stash, carries a non-empty conventional name. resolveIdentity
	// substitutes the last-known name for the path so a boot-only re-parse
	// deltas against the same save (via lastPushedSectionHashes) instead of
	// forking a new save under the "Unknown Player" fallback. Only names
	// produced by a parse's own saveName are recorded here — a substituted
	// or fallback name must never overwrite a real remembered name.
	lastKnownSaveNames map[string]string

	// parseFailures remembers failures that were successfully delivered, keyed
	// by path, so unchanged failures do not repeat on rescans.
	parseFailures map[string]parseFailure

	// dirUnitSnapshotHashes caches, per directory-unit save root, the
	// aggregate hash of the (relative member path, content hash) set from
	// the last attempted dispatch (see aggregateHash) — not "last
	// successfully parsed": the hash is recorded before parseAndPush runs
	// (see dispatchDirectoryUnit), so a failed parse still suppresses a
	// retry against an unchanged snapshot. pluginChanged is the invalidation
	// path: it clears the entries for a game's directories so a plugin fix
	// forces a re-parse regardless of this dedup. When a directory-unit
	// event fires and the freshly computed snapshot hashes to the same
	// value, the parse is skipped entirely — the aggregate dedup layered
	// above the watcher's own per-file quiescence.
	dirUnitSnapshotHashes map[string][32]byte

	// regexPatterns caches compiled file-pattern regular expressions.
	regexPatterns regexPatternCache

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

// sectionHashCache holds the per-section SHA-256 hashes from the last
// successful push for a file path, along with the save identity (saveName)
// those hashes were computed under. See lastPushedSectionHashes.
type sectionHashCache struct {
	gameID   string
	saveName string
	hashes   map[string][32]byte
}

type parseFailure struct {
	gameID      string
	contentHash [32]byte
	errorType   pb.ParseErrorType
	message     string
}

type regexPatternCache struct {
	patterns sync.Map // map[string]*cachedRegexPattern
}

type cachedRegexPattern struct {
	once sync.Once
	re   *regexp.Regexp
}

func (c *regexPatternCache) matches(pattern, name string) bool {
	entry := &cachedRegexPattern{}
	cached, _ := c.patterns.LoadOrStore(pattern, entry)
	entry, ok := cached.(*cachedRegexPattern)
	if !ok {
		return false
	}
	entry.once.Do(func() {
		compiled, err := regexp.Compile(strings.TrimPrefix(pattern, "regex:"))
		if err != nil {
			slog.Default().Warn("invalid regex file pattern", "pattern", pattern, "error", err)
			return
		}
		entry.re = compiled
	})
	return entry.re != nil && entry.re.MatchString(name)
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
		log = slog.New(slog.DiscardHandler)
	}
	return &Daemon{
		cfg:                     cfg,
		fs:                      fsys,
		watcher:                 watcher,
		runner:                  runner,
		ws:                      ws,
		plugins:                 plugins,
		updater:                 updater,
		log:                     log,
		exitFunc:                os.Exit,
		restartFunc:             func(string, string) error { return nil },
		watchedDirs:             make(map[string]string),
		allowedSaveRoots:        defaultSaveRoots(),
		configDir:               defaultConfigDir(),
		pendingLinkCode:         make(chan linkCodeResult, 1),
		pluginUpdateResetCh:     make(chan struct{}, 1),
		lastPushedSectionHashes: make(map[string]*sectionHashCache),
		lastKnownSaveNames:      make(map[string]string),
		parseFailures:           make(map[string]parseFailure),
		pluginGenerations:       make(map[string]uint64),
		dirUnitSnapshotHashes:   make(map[string][32]byte),
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
			d.pluginChanged(ctx, gameID, gameCfg)
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

// pluginUnitInfo returns gameID's plugin manifest entry, carrying the
// directory-unit fields (Unit, Members) alongside everything else. ok is
// false when no plugin manager is configured, the manifest is unavailable,
// or the game is unknown to it — callers must treat that identically to a
// file-unit plugin, so directory-unit behavior never regresses a file-unit
// plugin when the manifest can't be consulted.
func (d *Daemon) pluginUnitInfo(ctx context.Context, gameID string) (pluginmgr.PluginInfo, bool) {
	if d.plugins == nil {
		return pluginmgr.PluginInfo{}, false
	}
	manifests, err := d.plugins.Manifests(ctx)
	if err != nil {
		return pluginmgr.PluginInfo{}, false
	}
	info, ok := manifests[gameID]
	return info, ok
}

// isDirectoryUnit reports whether info (as returned by pluginUnitInfo)
// describes a directory-unit save plugin.
func isDirectoryUnit(info pluginmgr.PluginInfo, ok bool) bool {
	return ok && info.Unit == unitDirectory
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

	// Start the connection in the background. Establishing it is not a
	// precondition for running: a transient failure (DNS not ready at boot,
	// server unreachable) is retried in-process and never fatal. The daemon
	// announces online — and re-announces — whenever a connection is
	// established, via the Connected() signal in the event loop below.
	d.ws.Start(ctx)
	defer func() {
		if closeErr := d.ws.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("ws close: %w", closeErr)
		}
	}()

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
		case <-d.ws.Connected():
			d.log.InfoContext(ctx, "websocket connected, announcing online")
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
		d.mu.RLock()
		gameCfg, ok := d.cfg.Games[gameID]
		d.mu.RUnlock()
		if ok {
			d.pluginChanged(ctx, gameID, gameCfg)
		}
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

	d.pluginChanged(ctx, gameID, gameCfg)
}

// pluginChanged invalidates all state derived from a changed plugin, then
// reparses the game's tracked saves so the next push is a complete snapshot.
// For a directory-unit game this must branch to rescanQuietDirectoryUnit
// (mirroring scanGame's own branch) rather than falling through to
// rescanQuiet: rescanQuiet's ReadDir+filterSaveFiles path would dispatch raw
// member files under file identities to a plugin that expects a single
// tar-archived directory identity. The stale dirUnitSnapshotHashes entries
// for this game's watched directories are cleared first so the forced
// re-parse below isn't itself suppressed by the aggregate dedup.
func (d *Daemon) pluginChanged(ctx context.Context, gameID string, cfg GameConfig) {
	d.mu.Lock()
	d.pluginGenerations[gameID]++
	for path, cache := range d.lastPushedSectionHashes {
		if cache.gameID == gameID {
			delete(d.lastPushedSectionHashes, path)
		}
	}
	for path, failure := range d.parseFailures {
		if failure.gameID == gameID {
			delete(d.parseFailures, path)
		}
	}
	for dir, dirGameID := range d.watchedDirs {
		if dirGameID == gameID {
			delete(d.dirUnitSnapshotHashes, dir)
		}
	}
	d.mu.Unlock()

	if info, ok := d.pluginUnitInfo(ctx, gameID); isDirectoryUnit(info, ok) {
		d.rescanQuietDirectoryUnit(ctx, gameID, cfg, info)
		return
	}
	d.rescanQuiet(ctx, gameID, cfg)
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
			pluginDownloadFailedMessage,
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

// tryDiscoverCandidates expands a path template into candidates and returns
// the first candidate that has valid directories with at least one matching
// save file. Returns ("", 0) if no candidate has matching saves.
func (d *Daemon) tryDiscoverCandidates(
	pathTemplate string, info pluginmgr.PluginInfo,
) (string, int) {
	for _, expanded := range expandPaths(pathTemplate) {
		dirs := resolveGlob(d.fs, expanded, info.ExcludeDirs)
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
		if anyValid && totalMatching > 0 {
			return expanded, totalMatching
		}
	}
	return "", 0
}

// tryDiscoverDirectoryUnitCandidates is tryDiscoverCandidates' counterpart
// for directory-unit plugins: each resolved directory that exists and
// contains at least one Members match is one save (see the SaveCount
// semantics in scanGame), rather than counting individual matching files.
func (d *Daemon) tryDiscoverDirectoryUnitCandidates(
	pathTemplate string, info pluginmgr.PluginInfo,
) (string, int) {
	for _, expanded := range expandPaths(pathTemplate) {
		dirs := resolveGlob(d.fs, expanded, info.ExcludeDirs)
		anyValid := false
		matching := 0
		for _, dir := range dirs {
			stat, statErr := d.fs.Stat(dir)
			if statErr != nil || !stat.IsDir() {
				continue
			}
			anyValid = true
			if len(d.directoryUnitMembers(dir, info.Members, info.ExcludeDirs)) > 0 {
				matching++
			}
		}
		if anyValid && matching > 0 {
			return expanded, matching
		}
	}
	return "", 0
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

		var bestExpanded string
		var bestMatching int
		if info.Unit == unitDirectory {
			bestExpanded, bestMatching = d.tryDiscoverDirectoryUnitCandidates(pathTemplate, info)
		} else {
			bestExpanded, bestMatching = d.tryDiscoverCandidates(pathTemplate, info)
		}
		if bestExpanded == "" {
			continue
		}

		d.log.InfoContext(ctx, "game discovered",
			slog.String("game_id", gameID),
			slog.String("name", info.Name),
			slog.String("path", bestExpanded),
			slog.Int("file_count", bestMatching),
		)
		discovered = append(discovered, DiscoveredGame{
			GameID:         gameID,
			Name:           info.Name,
			Path:           bestExpanded,
			FileCount:      bestMatching,
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

// rescanQuietDirectoryUnit is rescanQuiet's counterpart for directory-unit
// games: re-dispatches each already-watched save directory. The aggregate
// dedup in dispatchDirectoryUnit suppresses any directory whose member
// snapshot did not change, so a reconnect that finds nothing changed pushes
// nothing.
func (d *Daemon) rescanQuietDirectoryUnit(
	ctx context.Context, gameID string, cfg GameConfig, info pluginmgr.PluginInfo,
) bool {
	dirs := resolveGlob(d.fs, cfg.SavePath, cfg.ExcludeDirs)

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
		if stat, statErr := d.fs.Stat(dir); statErr != nil || !stat.IsDir() {
			continue
		}
		relPaths := d.directoryUnitMembers(dir, info.Members, cfg.ExcludeDirs)
		d.dispatchDirectoryUnit(ctx, gameID, dir, relPaths, true)
	}
	return true
}

// matchesMember reports whether rel (a slash-separated file path relative to
// a save directory) matches any of the given include patterns. Members are
// authored with '/' (e.g. "Players/*.sav"), so matching uses path.Match
// (slash-only glob semantics) rather than filepath.Match, which on Windows
// would otherwise compare against OS-separator paths and silently drop every
// nested member. A nil/empty members list matches every file — the default
// for plugins that don't need to narrow the archived set beyond
// exclude_dirs.
func matchesMember(rel string, members []string) bool {
	if len(members) == 0 {
		return true
	}
	for _, pattern := range members {
		if matched, err := path.Match(pattern, rel); err == nil && matched {
			return true
		}
	}
	return false
}

// directoryUnitMembers walks dir (respecting excludeDirs, matching the
// daemon's existing case-insensitive exclude semantics) and returns the
// sorted, slash-separated paths — relative to dir — of every file matching
// one of the members include patterns. Recursion is driven by the ReadDir
// entry's own IsDir(), never a followed stat: this matches the watcher's
// parallel walk (watcher.collectDirectoryUnitDirs), so a symlinked
// directory is neither recursed into nor archived, and a symlink cycle
// cannot recurse unbounded. A symlink entry (file or directory) is also
// never treated as a member itself and skipped outright: archiving it would
// read and tar whatever it happens to point at, including a target outside
// the save directory, silently pulling arbitrary host filesystem content
// into a save's tar snapshot.
func (d *Daemon) directoryUnitMembers(dir string, members, excludeDirs []string) []string {
	var matches []string
	var walk func(prefix string)
	walk = func(prefix string) {
		full := filepath.Join(dir, prefix)
		entries, err := d.fs.ReadDir(full)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.Type()&fs.ModeSymlink != 0 {
				continue
			}
			name := entry.Name()
			rel := filepath.ToSlash(filepath.Join(prefix, name))
			if entry.IsDir() {
				if isExcludedDir(name, excludeDirs) {
					continue
				}
				walk(filepath.Join(prefix, name))
				continue
			}
			if matchesMember(rel, members) {
				matches = append(matches, rel)
			}
		}
	}
	walk("")
	sort.Strings(matches)
	return matches
}

// dirUnitMemberSizeCap rejects any single directory-unit member whose
// content exceeds this many bytes: skipped with a logged warning rather than
// aborting the whole snapshot. Mirrors the plugin-side parser's own
// per-member cap (MAX_MEMBER_SIZE in
// plugins/palworld/parser/src/tarball.rs) so an oversized member is dropped
// on the producer side instead of merely failing the parser's bounded read.
const dirUnitMemberSizeCap = 64 * 1024 * 1024

// dirUnitTotalSizeCap aborts building a directory unit's snapshot once the
// running total of its (post-per-member-cap) member sizes exceeds this.
// Mirrors the parser's own aggregate cap (MAX_TOTAL_SIZE in
// plugins/palworld/parser/src/tarball.rs).
const dirUnitTotalSizeCap = 128 * 1024 * 1024

// dirUnitMember is one successfully read directory-unit member: its path
// relative to the save directory, its content, and the content's SHA-256.
type dirUnitMember struct {
	rel  string
	data []byte
	hash [32]byte
}

// readDirectoryUnitMembers reads and hashes each of relPaths (as returned by
// directoryUnitMembers) under dir, retaining the bytes it reads so a
// subsequent tar assembly (tarFromDirUnitMembers) never has to read a member
// twice. It applies the two producer-side size caps mirrored from the
// plugin parser (dirUnitMemberSizeCap, dirUnitTotalSizeCap) and tolerates
// individual read failures: a directory unit is read live, racing the game
// process that may still be writing one of its members, so a single
// unreadable member is skipped with a logged warning rather than aborting
// the whole snapshot. It aborts (returns an error) only when either the
// running total exceeds dirUnitTotalSizeCap, or every candidate member
// failed to read — a live race explains one bad member, never all of them.
func (d *Daemon) readDirectoryUnitMembers(
	ctx context.Context, dir string, relPaths []string,
) ([]dirUnitMember, error) {
	members := make([]dirUnitMember, 0, len(relPaths))
	var total int64
	for _, rel := range relPaths {
		data, readErr := d.fs.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if readErr != nil {
			d.log.WarnContext(ctx, "directory-unit member unreadable, skipping",
				slog.String("path", dir),
				slog.String("member", rel),
				slog.String("error", readErr.Error()),
			)
			continue
		}
		if int64(len(data)) > dirUnitMemberSizeCap {
			d.log.WarnContext(ctx, "directory-unit member exceeds size cap, skipping",
				slog.String("path", dir),
				slog.String("member", rel),
				slog.Int("size", len(data)),
				slog.Int("cap", dirUnitMemberSizeCap),
			)
			continue
		}
		total += int64(len(data))
		if total > dirUnitTotalSizeCap {
			return nil, fmt.Errorf(
				"directory unit %s exceeds total size cap of %d bytes", dir, dirUnitTotalSizeCap,
			)
		}
		members = append(members, dirUnitMember{rel: rel, data: data, hash: sha256.Sum256(data)})
	}
	if len(relPaths) > 0 && len(members) == 0 {
		return nil, fmt.Errorf("directory unit %s: no members could be read", dir)
	}
	return members, nil
}

// tarFromDirUnitMembers archives members into a POSIX ustar archive, one
// entry per member named by its path relative to the save directory. members
// must already be in deterministic sorted order — directoryUnitMembers sorts
// the relative paths it returns, and readDirectoryUnitMembers preserves that
// order.
func tarFromDirUnitMembers(members []dirUnitMember) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, member := range members {
		hdr := &tar.Header{
			Name: member.rel,
			Mode: dirUnitTarFileMode,
			Size: int64(len(member.data)),
		}
		if headerErr := tw.WriteHeader(hdr); headerErr != nil {
			return nil, fmt.Errorf("tar header %s: %w", member.rel, headerErr)
		}
		if _, writeErr := tw.Write(member.data); writeErr != nil {
			return nil, fmt.Errorf("tar write %s: %w", member.rel, writeErr)
		}
	}
	if closeErr := tw.Close(); closeErr != nil {
		return nil, fmt.Errorf("tar close: %w", closeErr)
	}
	return buf.Bytes(), nil
}

// aggregateHash reduces a directory unit's per-member content hashes to a
// single SHA-256 over their deterministic serialization: each sorted
// relative path is written length-prefixed (4-byte big-endian length, then
// the path bytes, then the raw 32-byte content hash) with no delimiter
// between entries — a delimited "relpath:hexhash\n" join would let a
// crafted member name (containing ':' or '\n', both legal filename bytes)
// collide two genuinely different snapshots onto the same hash. Comparing
// this scalar across scans is the aggregate (path,hash) dedup: an unchanged
// value means no member's content differs, and no member was added or
// removed, since the last attempted dispatch for this directory (see
// dispatchDirectoryUnit for what "attempted" means here).
func aggregateHash(memberHashes map[string][32]byte) [32]byte {
	names := make([]string, 0, len(memberHashes))
	for name := range memberHashes {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, name := range names {
		var nameLen [4]byte
		binary.BigEndian.PutUint32(
			nameLen[:],
			uint32(len(name)),
		) // #nosec G115 -- member relative paths, bounded by filesystem limits
		buf.Write(nameLen[:])
		buf.WriteString(name)
		h := memberHashes[name]
		buf.Write(h[:])
	}
	return sha256.Sum256(buf.Bytes())
}

// dispatchDirectoryUnit reads and hashes dir's current member snapshot
// (readDirectoryUnitMembers) and, unless the resulting aggregate is
// identical to the last attempted dispatch for this directory (checked and
// stored in the same locked section below — the compare-and-set must be
// atomic even though today's single event-loop goroutine makes the race
// theoretical; pluginChanged is what invalidates a stale entry after a
// plugin change), tar-archives the already-read member bytes and dispatches
// through the existing parseAndPush pipeline exactly like a file-unit save.
// Comparing before ever building a tar means an unchanged directory costs a
// read+hash pass but never a tar assembly; a changed directory costs exactly
// one read per member, never two.
//
// relPaths is dir's current member listing (see directoryUnitMembers):
// callers that already computed it while resolving save directories (see
// resolveDirectoryUnitSaveDirs) pass that same list through instead of
// walking dir a second time; callers reacting to a live filesystem change or
// a rescan compute it fresh immediately before calling.
//
// preloadedData bypasses parseAndPush's ReadFile branch (the tar bytes are
// already in hand), fullPath is the directory itself — the cache/identity
// key a directory-unit save is tracked under — and fileName is the
// directory's own base name, per the Runner.Run(gameID, fileName, saveBytes)
// contract (fileName = dir name for directory units).
func (d *Daemon) dispatchDirectoryUnit(
	ctx context.Context, gameID, dir string, relPaths []string, quiet bool,
) {
	members, err := d.readDirectoryUnitMembers(ctx, dir, relPaths)
	if err != nil {
		d.log.ErrorContext(ctx, "failed to build directory-unit archive",
			slog.String("game_id", gameID),
			slog.String("path", dir),
			slog.String("error", err.Error()),
		)
		return
	}

	memberHashes := make(map[string][32]byte, len(members))
	for _, member := range members {
		memberHashes[member.rel] = member.hash
	}
	agg := aggregateHash(memberHashes)

	d.mu.Lock()
	prevAgg, seen := d.dirUnitSnapshotHashes[dir]
	if seen && agg == prevAgg {
		d.mu.Unlock()
		d.log.DebugContext(ctx, "directory unit unchanged, skipping re-parse",
			slog.String("game_id", gameID),
			slog.String("path", dir),
		)
		return
	}
	d.dirUnitSnapshotHashes[dir] = agg
	d.mu.Unlock()

	tarBytes, tarErr := tarFromDirUnitMembers(members)
	if tarErr != nil {
		d.log.ErrorContext(ctx, "failed to build directory-unit archive",
			slog.String("game_id", gameID),
			slog.String("path", dir),
			slog.String("error", tarErr.Error()),
		)
		return
	}

	d.parseAndPush(ctx, gameID, dir, filepath.Base(dir), tarBytes, quiet)
}

func (d *Daemon) scanGame(
	ctx context.Context, gameID string, cfg GameConfig, quiet bool,
) {
	if info, ok := d.pluginUnitInfo(ctx, gameID); isDirectoryUnit(info, ok) {
		d.scanDirectoryUnitGame(ctx, gameID, cfg, info, quiet)
		return
	}

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

// scanDirectoryUnitGame is scanGame's counterpart for directory-unit
// plugins. Each resolved default_paths directory containing at least one
// Members match is itself one save (SaveCount counts directories, never
// member files — see aggregateHash/dispatchDirectoryUnit for how a save's
// contents are archived and deduplicated). Every such directory is watched
// recursively (watcher.AddDirectoryUnit) so a nested member write (e.g.
// under Players/) is coalesced into a single event addressed to the save
// root, which handleFileEvent then routes back to this same dispatch path.
func (d *Daemon) scanDirectoryUnitGame(
	ctx context.Context, gameID string, cfg GameConfig, info pluginmgr.PluginInfo, quiet bool,
) {
	if quiet && d.rescanQuietDirectoryUnit(ctx, gameID, cfg, info) {
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

	validDirs, saveDirs, saveDirMembers := d.resolveDirectoryUnitSaveDirs(dirs, info.Members, cfg.ExcludeDirs)

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

	saveNames := make([]string, len(saveDirs))
	for i, dir := range saveDirs {
		saveNames[i] = filepath.Base(dir)
	}

	d.log.InfoContext(ctx, "save directories found",
		slog.String("game", displayName),
		slog.String("game_id", gameID),
		slog.Int("count", len(saveDirs)),
		slog.String("path", cfg.SavePath),
	)

	// A directory-unit save counts as exactly one save/file, regardless of
	// how many member files it contains.
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ScanCompleted{ScanCompleted: &pb.ScanCompleted{
		GameId:     gameID,
		Path:       cfg.SavePath,
		FilesFound: int32(len(saveDirs)), // #nosec G115 -- bounded by filesystem limits
		FileNames:  saveNames,
	}}})

	if len(saveDirs) == 0 {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: dirs,
		}}})
		return
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameDetected{GameDetected: &pb.GameDetected{
		GameId:    gameID,
		Path:      cfg.SavePath,
		SaveCount: int32(len(saveDirs)), // #nosec G115 -- bounded by filesystem limits
	}}})

	d.watchAndDispatchDirectoryUnits(ctx, gameID, displayName, cfg, saveDirs, saveDirMembers)
}

// resolveDirectoryUnitSaveDirs classifies the resolved candidate
// directories: validDirs counts every one that exists, and saveDirs is the
// subset that additionally contains at least one Members match — the
// directories that are actually saves (see scanDirectoryUnitGame). It also
// returns the member listing (directoryUnitMembers) it already walked for
// each saveDir, keyed by directory, so watchAndDispatchDirectoryUnits can
// thread it straight into dispatchDirectoryUnit instead of walking dir a
// second time.
func (d *Daemon) resolveDirectoryUnitSaveDirs(
	dirs, members, excludeDirs []string,
) (validDirs int, saveDirs []string, saveDirMembers map[string][]string) {
	saveDirMembers = make(map[string][]string)
	for _, dir := range dirs {
		stat, statErr := d.fs.Stat(dir)
		if statErr != nil || !stat.IsDir() {
			continue
		}
		validDirs++
		relPaths := d.directoryUnitMembers(dir, members, excludeDirs)
		if len(relPaths) > 0 {
			saveDirs = append(saveDirs, dir)
			saveDirMembers[dir] = relPaths
		}
	}
	return validDirs, saveDirs, saveDirMembers
}

// watchAndDispatchDirectoryUnits recursively watches every resolved save
// directory (see watcher.AddDirectoryUnit) and dispatches its initial member
// snapshot through the shared parseAndPush pipeline. saveDirMembers is the
// per-directory member listing resolveDirectoryUnitSaveDirs already walked;
// reusing it here avoids a redundant directoryUnitMembers walk immediately
// after resolution.
func (d *Daemon) watchAndDispatchDirectoryUnits(
	ctx context.Context, gameID, displayName string, cfg GameConfig,
	saveDirs []string, saveDirMembers map[string][]string,
) {
	for _, dir := range saveDirs {
		if watchErr := d.watcher.AddDirectoryUnit(dir, cfg.ExcludeDirs); watchErr != nil {
			continue
		}
		d.mu.Lock()
		d.watchedDirs[dir] = gameID
		d.mu.Unlock()
	}

	d.log.InfoContext(
		ctx,
		"watching game",
		slog.String("game", displayName),
		slog.String("game_id", gameID),
		slog.String("path", cfg.SavePath),
		slog.Int("save_count", len(saveDirs)),
	)
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_Watching{Watching: &pb.Watching{
		GameId:         gameID,
		Path:           cfg.SavePath,
		FilesMonitored: int32(len(saveDirs)), // #nosec G115 -- bounded by filesystem limits
	}}})

	for _, dir := range saveDirs {
		d.dispatchDirectoryUnit(ctx, gameID, dir, saveDirMembers[dir], false)
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
		d.mu.Lock()
		delete(d.parseFailures, ev.Path)
		d.mu.Unlock()
		return
	}

	// Directory-unit events address the save root directly: the watcher
	// coalesces every nested member change beneath a directory-unit root
	// into one FileEvent for the root itself (see watcher.AddDirectoryUnit),
	// so ev.Path IS the watched directory here — unlike a file-unit event,
	// whose ev.Path is always a file inside the watched directory. Check
	// this before the file-unit parent-directory lookup below; a genuine
	// file-unit event can never collide with it, since ev.Path there is
	// always a file whose parent (not itself) is the watched directory.
	d.mu.RLock()
	rootGameID, isRoot := d.watchedDirs[ev.Path]
	d.mu.RUnlock()
	if isRoot {
		if info, ok := d.pluginUnitInfo(ctx, rootGameID); isDirectoryUnit(info, ok) {
			d.mu.RLock()
			gameCfg := d.cfg.Games[rootGameID]
			d.mu.RUnlock()
			relPaths := d.directoryUnitMembers(ev.Path, info.Members, gameCfg.ExcludeDirs)
			d.dispatchDirectoryUnit(ctx, rootGameID, ev.Path, relPaths, false)
			return
		}
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
	if len(gameCfg.FileExtensions) > 0 && !matchesExtension(ext, gameCfg.FileExtensions) {
		return
	}
	if len(gameCfg.FilePatterns) > 0 && !d.matchesPattern(fileName, gameCfg.FilePatterns) {
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
	d.mu.RLock()
	generation := d.pluginGenerations[gameID]
	d.mu.RUnlock()
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
		d.handleParseFailure(ctx, gameID, fullPath, fileName, saveBytes, err, generation)
		return
	}
	d.mu.Lock()
	if d.pluginGenerations[gameID] == generation {
		delete(d.parseFailures, fullPath)
	}
	d.mu.Unlock()

	// Resolve the save identity once, before anything downstream consumes
	// it, so ParseCompleted and the push (and the delta cache it consults)
	// all agree on the same name. See resolveIdentity.
	state.Identity = d.resolveIdentity(fullPath, state.Identity)

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

	d.pushState(ctx, gameID, fullPath, state, generation)
}

func (d *Daemon) handleParseFailure(
	ctx context.Context,
	gameID, fullPath, fileName string,
	saveBytes []byte,
	err error,
	generation ...uint64,
) {
	gen := uint64(0)
	if len(generation) > 0 {
		gen = generation[0]
	}
	d.mu.RLock()
	current := d.pluginGenerations[gameID]
	d.mu.RUnlock()
	if current != gen {
		return
	}
	errorType := "PARSE_ERROR_TYPE_PARSE_ERROR"
	if pluginErr, ok := errors.AsType[*PluginError](err); ok {
		errorType = pluginErr.Type
	}
	failure := parseFailure{
		gameID:      gameID,
		contentHash: sha256.Sum256(saveBytes),
		errorType:   toParseErrorType(errorType),
		message:     err.Error(),
	}
	d.mu.Lock()
	previous, ok := d.parseFailures[fullPath]
	d.mu.Unlock()
	if ok && previous.gameID == gameID && previous == failure {
		d.log.DebugContext(
			ctx,
			"suppressed repeated plugin parse failure",
			slog.String("game_id", gameID),
			slog.String("file_name", fileName),
		)
		return
	}
	d.log.ErrorContext(
		ctx,
		"plugin parse failed",
		slog.String("game_id", gameID),
		slog.String("file_name", fileName),
		slog.String("error_type", errorType),
		slog.String("error", err.Error()),
	)
	msg := &pb.Message{Payload: &pb.Message_ParseFailed{ParseFailed: &pb.ParseFailed{
		GameId: gameID, FileName: fileName, ErrorType: failure.errorType, Message: err.Error(),
	}}}
	d.mu.RLock()
	current = d.pluginGenerations[gameID]
	d.mu.RUnlock()
	if current != gen {
		return
	}
	if sendErr := d.sendMessageReturningError(ctx, msg); sendErr == nil {
		d.mu.Lock()
		if d.pluginGenerations[gameID] == gen {
			d.parseFailures[fullPath] = failure
		}
		d.mu.Unlock()
	} else {
		d.log.WarnContext(ctx, "failed to send message", slog.String("error", sendErr.Error()))
	}
}

// resolveIdentity substitutes a sticky save name when the parse yielded an
// empty saveName. The plugin contract treats empty as "identity unknown"
// (e.g. a boot-only MTGA Player.log with no login event yet) — every real
// save, including game-scoped saves, carries a non-empty conventional name.
// The daemon prefers the most recently seen non-empty saveName for this
// file path, falling back to unknownSaveName only if none exists.
// lastKnownSaveNames is updated only when the parse itself produced a
// non-empty saveName, so a substituted or fallback name can never overwrite
// a real remembered name.
func (d *Daemon) resolveIdentity(filePath string, identity Identity) Identity {
	d.mu.Lock()
	defer d.mu.Unlock()
	if identity.SaveName != "" {
		d.lastKnownSaveNames[filePath] = identity.SaveName
		return identity
	}
	if known, ok := d.lastKnownSaveNames[filePath]; ok {
		identity.SaveName = known
	} else {
		identity.SaveName = unknownSaveName
	}
	return identity
}

func (d *Daemon) pushState(
	ctx context.Context, gameID, filePath string, state *GameState, generation ...uint64,
) {
	gen := uint64(0)
	if len(generation) > 0 {
		gen = generation[0]
	}
	d.mu.RLock()
	current := d.pluginGenerations[gameID]
	d.mu.RUnlock()
	if current != gen {
		return
	}
	sections := d.protoSections(ctx, gameID, state.Sections)

	// Sort sections by name for deterministic hashing (map iteration order is random).
	slices.SortFunc(sections, func(a, b *pb.GameSection) int {
		return strings.Compare(a.Name, b.Name)
	})

	changed, newHashes := d.filterChangedSections(ctx, gameID, filePath, state.Identity.SaveName, sections)
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

	d.sendState(ctx, gameID, filePath, state, changed, sections, newHashes, gen)
}

func (d *Daemon) protoSections(
	ctx context.Context, gameID string, source map[string]Section,
) []*pb.GameSection {
	sections := make([]*pb.GameSection, 0, len(source))
	for name, section := range source {
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
	return sections
}

func (d *Daemon) sendState(
	ctx context.Context, gameID, filePath string, state *GameState,
	changed, sections []*pb.GameSection, newHashes map[string][32]byte, gen uint64,
) {
	allNames := make([]string, len(sections))
	for i, section := range sections {
		allNames[i] = section.Name
	}

	pushSave := &pb.PushSave{
		Identity:        toProtoIdentity(state.Identity),
		Summary:         state.Summary,
		Sections:        changed,
		GameId:          gameID,
		ParsedAt:        timestamppb.Now(),
		AllSectionNames: allNames,
		FileName:        filepath.Base(filePath),
	}
	opts := proto.MarshalOptions{Deterministic: true}
	msg := &pb.Message{Payload: &pb.Message_PushSave{PushSave: pushSave}}
	data, err := opts.Marshal(msg)
	if err != nil {
		d.log.ErrorContext(ctx, "failed to marshal PushSave message",
			slog.String("game_id", gameID),
			slog.String("error", err.Error()),
		)
		return
	}
	d.mu.RLock()
	current := d.pluginGenerations[gameID]
	d.mu.RUnlock()
	if current != gen {
		return
	}
	if sendErr := d.sendCompressed(data); sendErr != nil {
		d.log.WarnContext(ctx, "failed to send message", slog.String("error", sendErr.Error()))
		return
	}
	d.mu.Lock()
	if d.pluginGenerations[gameID] == gen {
		d.lastPushedSectionHashes[filePath] = &sectionHashCache{
			gameID: gameID, saveName: state.Identity.SaveName, hashes: newHashes,
		}
	}
	d.mu.Unlock()
}

// filterChangedSections hashes each section individually and returns only those
// whose content differs from the last successful push for this file path under
// the same save identity. If the cached entry belongs to a different saveName
// (or there is no cached entry), the cache is treated as cold and every section
// is reported as changed — see lastPushedSectionHashes for why.
func (d *Daemon) filterChangedSections(
	ctx context.Context, gameID, filePath, saveName string, sections []*pb.GameSection,
) ([]*pb.GameSection, map[string][32]byte) {
	opts := proto.MarshalOptions{Deterministic: true}
	var prevHashes map[string][32]byte
	d.mu.RLock()
	cached := d.lastPushedSectionHashes[filePath]
	d.mu.RUnlock()
	if cached != nil && cached.gameID == gameID && cached.saveName == saveName {
		prevHashes = cached.hashes
	}
	newHashes := make(map[string][32]byte, len(sections))
	var changed []*pb.GameSection

	for _, section := range sections {
		sectionBytes, err := opts.Marshal(section)
		if err != nil {
			d.log.ErrorContext(ctx, "failed to marshal section for hashing",
				slog.String("game_id", gameID),
				slog.String("section", section.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		h := sha256.Sum256(sectionBytes)
		newHashes[section.Name] = h
		if prev, ok := prevHashes[section.Name]; !ok || prev != h {
			changed = append(changed, section)
		}
	}
	return changed, newHashes
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
				Message: pluginDownloadFailedMessage,
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
	d.mu.RLock()
	gameCfg, ok := d.cfg.Games[msg.GameId]
	d.mu.RUnlock()
	if ok {
		d.pluginChanged(ctx, msg.GameId, gameCfg)
	}

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

// defaultSaveRoots returns the locally-computed allowlist of directory roots
// the daemon may read/enumerate for saves: the user's home subtree plus a
// small per-OS set of known game/save roots. The server may select paths
// WITHIN these roots but can never cause the daemon to escape them
// (finding 4.3 / R12). Empty result means nothing is allowed (fail closed).
func defaultSaveRoots() []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Clean(home))
	}
	if runtime.GOOS == "linux" {
		// Steam Deck / Linux removable media (Steam libraries on SD cards).
		roots = append(roots, "/run/media", "/media", "/mnt")
	}
	// Windows: home (%USERPROFILE%) covers %APPDATA%/%LOCALAPPDATA%; extra
	// fixed drives are intentionally NOT blanket-allowed. macOS: ~/Library is
	// under home. Home is the safe default on both.
	return roots
}

// storeGameCfg atomically replaces the config for gameID, returning the
// previous config and whether it existed.
func (d *Daemon) storeGameCfg(gameID string, c GameConfig) (GameConfig, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	old, existed := d.cfg.Games[gameID]
	d.cfg.Games[gameID] = c
	return old, existed
}

// refuseDisallowedSavePath returns a refusal result (and refused=true) when a
// resolved SavePath escapes the local save-root allowlist (finding 4.3 / R12).
func (d *Daemon) refuseDisallowedSavePath(
	ctx context.Context, gameID, resolvedPath string,
) (configGameResult, bool) {
	if resolvedPath == "" || d.saveRootAllowed(resolvedPath) {
		return configGameResult{}, false
	}
	d.log.WarnContext(ctx, "config save path outside allowed roots, refusing game",
		slog.String("game_id", gameID),
		slog.String("path", resolvedPath),
	)
	return configGameResult{
		Error:        "save path outside allowed roots",
		ResolvedPath: resolvedPath,
	}, true
}

// saveRootAllowed reports whether path (already expanded) resolves, after
// symlink resolution of its deepest existing ancestor, inside one of the
// locally-computed allowed save roots. Containment uses a separator boundary
// so /home/u-evil does not match root /home/u.
func (d *Daemon) saveRootAllowed(path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	resolved := d.resolveDeepest(abs)
	for _, root := range d.allowedSaveRoots {
		rc, absErr := filepath.Abs(filepath.Clean(root))
		if absErr != nil {
			continue
		}
		if linked, e := d.fs.EvalSymlinks(rc); e == nil {
			rc = linked
		}
		sep := string(filepath.Separator)
		boundary := rc
		if !strings.HasSuffix(boundary, sep) {
			boundary += sep
		}
		if resolved == rc || strings.HasPrefix(resolved, boundary) {
			return true
		}
	}
	return false
}

// resolveDeepest resolves symlinks on the deepest existing ancestor of abs
// and re-appends the non-existent remainder, so a not-yet-created save dir is
// still checked against its real (symlink-resolved) location.
func (d *Daemon) resolveDeepest(abs string) string {
	current := abs
	var suffix string
	for {
		if _, err := d.fs.Stat(current); err == nil {
			linked, evalErr := d.fs.EvalSymlinks(current)
			if evalErr != nil {
				linked = current
			}
			if suffix == "" {
				return linked
			}
			return filepath.Join(linked, suffix)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs // reached the FS root, nothing exists
		}
		suffix = filepath.Join(filepath.Base(current), suffix)
		current = parent
	}
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
		resolvedPath := d.resolveFirstValid(newGame.SavePath, newGame.ExcludeDirs)

		// Defense-in-depth: a compromised server must not point SavePath at
		// arbitrary local files (read + upload). Refuse any game whose
		// resolved path escapes the local save-root allowlist; never store,
		// scan, or watch it (finding 4.3 / R12).
		if res, refused := d.refuseDisallowedSavePath(ctx, gameID, resolvedPath); refused {
			results[gameID] = res
			continue
		}

		gameCfg := GameConfig{
			SavePath:       resolvedPath,
			Enabled:        newGame.Enabled,
			FileExtensions: newGame.FileExtensions,
			FilePatterns:   newGame.FilePatterns,
			ExcludeDirs:    newGame.ExcludeDirs,
			ExcludeSaves:   newGame.ExcludeSaves,
		}

		oldCfg, existed := d.storeGameCfg(gameID, gameCfg)

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
				results[gameID] = configGameResult{Error: pluginDownloadFailedMessage, ResolvedPath: resolvedPath}
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
				results[gameID] = configGameResult{Error: pluginDownloadFailedMessage, ResolvedPath: resolvedPath}
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

	d.evictUnwatchedPathCaches(dirs)
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

// evictUnwatchedPathCaches removes per-file state for the supplied
// directories, including a directory-unit's own aggregate snapshot cache
// (dirUnitSnapshotHashes is keyed by the directory itself, not a child path
// within it — see pathInDirectories). d.mu must be held by the caller.
func (d *Daemon) evictUnwatchedPathCaches(dirs []string) {
	for filePath := range d.lastPushedSectionHashes {
		if pathInDirectories(filePath, dirs) {
			delete(d.lastPushedSectionHashes, filePath)
		}
	}
	for filePath := range d.lastKnownSaveNames {
		if pathInDirectories(filePath, dirs) {
			delete(d.lastKnownSaveNames, filePath)
		}
	}
	for filePath := range d.parseFailures {
		if pathInDirectories(filePath, dirs) {
			delete(d.parseFailures, filePath)
		}
	}
	for dir := range d.dirUnitSnapshotHashes {
		if pathInDirectories(dir, dirs) {
			delete(d.dirUnitSnapshotHashes, dir)
		}
	}
}

// pathInDirectories reports whether filePath is one of dirs (a
// directory-unit save is cached under its own directory path — see
// dirUnitSnapshotHashes) or lies inside one of them.
func pathInDirectories(filePath string, dirs []string) bool {
	for _, dir := range dirs {
		if filePath == dir || strings.HasPrefix(filePath, dir+"/") || strings.HasPrefix(filePath, dir+"\\") {
			return true
		}
	}
	return false
}

func (d *Daemon) handleTestPath(ctx context.Context, gameID, path string) {
	d.mu.RLock()
	gameCfg := d.cfg.Games[gameID]
	d.mu.RUnlock()

	dirs := resolveGlob(d.fs, path, gameCfg.ExcludeDirs)
	var allFileNames []string
	for _, dir := range dirs {
		// Defense-in-depth: never enumerate a directory the server pointed us
		// at if it resolves outside the local save-root allowlist (finding
		// 4.3 / R12). This blocks the server-driven home-dir enumeration
		// primitive without trusting any server-supplied path.
		if !d.saveRootAllowed(dir) {
			d.log.WarnContext(ctx, "test path outside allowed save roots, refusing to enumerate",
				slog.String("game_id", gameID),
				slog.String("path", dir),
			)
			continue
		}
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
		if len(extensions) > 0 && !matchesExtension(ext, extensions) {
			continue
		}
		if len(patterns) > 0 && !d.matchesPattern(name, patterns) {
			continue
		}
		if isExcludedSave(name, excludeSaves) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// matchesPattern checks if filename matches any pattern. Patterns use glob
// semantics by default; a pattern prefixed with "regex:" is matched as a
// regular expression instead.
func (d *Daemon) matchesPattern(name string, patterns []string) bool {
	for _, pat := range patterns {
		if strings.HasPrefix(pat, "regex:") {
			if d.regexPatterns.matches(pat, name) {
				return true
			}
			continue
		}
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
	if err := d.sendMessageReturningError(ctx, msg); err != nil {
		d.log.WarnContext(ctx, "failed to send message", slog.String("error", err.Error()))
	}
}

func (d *Daemon) sendMessageReturningError(ctx context.Context, msg *pb.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		d.log.ErrorContext(ctx, "failed to marshal proto message", slog.String("error", err.Error()))
		return fmt.Errorf("marshal proto message: %w", err)
	}
	return d.sendCompressed(data)
}

// sendCompressed gzip-compresses data and sends it over the WebSocket.
func (d *Daemon) sendCompressed(data []byte) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return errors.Join(fmt.Errorf("gzip write: %w", err), gz.Close())
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	if err := d.ws.Send(buf.Bytes()); err != nil {
		return fmt.Errorf("ws send: %w", err)
	}
	return nil
}

// toParseErrorType converts a string error type to the proto enum.
func toParseErrorType(errorType string) pb.ParseErrorType {
	switch errorType {
	case pluginErrorTypeUnsupportedVersion:
		return pb.ParseErrorType_PARSE_ERROR_TYPE_UNSUPPORTED_VERSION
	case pluginErrorTypeCorruptFile:
		return pb.ParseErrorType_PARSE_ERROR_TYPE_CORRUPT_FILE
	case pluginErrorTypeParseError:
		return pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR
	case PluginErrorTypeResourceLimit:
		return pb.ParseErrorType_PARSE_ERROR_TYPE_RESOURCE_LIMIT
	}
	if v, ok := pb.ParseErrorType_value[errorType]; ok {
		return pb.ParseErrorType(v)
	}
	return pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR
}

// toProtoIdentity converts a daemon Identity to a proto SaveIdentity.
func toProtoIdentity(id Identity) *pb.SaveIdentity {
	si := &pb.SaveIdentity{Name: id.SaveName, DisplayName: id.DisplayName}
	if len(id.Extra) > 0 {
		extra, err := structpb.NewStruct(id.Extra)
		if err == nil {
			si.Extra = extra
		}
	}
	return si
}
