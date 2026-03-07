// Package daemon coordinates file watching, plugin execution, and server communication.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
	"google.golang.org/protobuf/proto"
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
	SourceID   string
	SourceUUID string
	Version    string
	BinaryPath string
	Retry      RetryConfig
	Games      map[string]GameConfig
}

// GameConfig holds per-game configuration.
type GameConfig struct {
	SavePath       string   `json:"savePath"`
	FileExtensions []string `json:"fileExtensions"`
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
	Reconnected() <-chan struct{}
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
	GameID         string   `json:"gameId"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	FileCount      int      `json:"fileCount"`
	FileExtensions []string `json:"fileExtensions"`
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
	log     *slog.Logger

	// exitFunc is called after a successful self-update to terminate
	// the process. Defaults to os.Exit; overridden in tests.
	exitFunc func(int)

	// mu protects watchedDirs and cfg.Games from concurrent access
	// (event loop goroutine vs. diagnostic HTTP handler).
	mu sync.RWMutex

	// Maps watched directory -> game ID.
	watchedDirs map[string]string

	// configDir is the directory for persisting config cache.
	// Defaults to os.UserConfigDir()/savecraft; empty disables caching.
	configDir string

	startTime time.Time
}

// New creates a Daemon with the given dependencies.
// A nil logger is replaced with a no-op logger.
func New(
	cfg Config,
	fsys FS,
	watcher Watcher,
	runner Runner,
	pusher PushClient,
	ws WSClient,
	plugins PluginManager,
	updater Updater,
	log *slog.Logger,
) *Daemon {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Daemon{
		cfg:         cfg,
		fs:          fsys,
		watcher:     watcher,
		runner:      runner,
		pusher:      pusher,
		ws:          ws,
		plugins:     plugins,
		updater:     updater,
		log:         log,
		exitFunc:    os.Exit,
		watchedDirs: make(map[string]string),
		configDir:   defaultConfigDir(),
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

	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.InfoContext(ctx, "daemon shutting down")
			d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOffline{SourceOffline: &pb.SourceOffline{
				Timestamp: timestamppb.Now(),
			}}})
			return nil
		case ev := <-d.watcher.Events():
			d.handleFileEvent(ctx, ev)
		case msg := <-d.ws.Messages():
			d.handleCommand(ctx, msg)
		case <-updateCh:
			d.checkPluginUpdates(ctx)
		case <-selfUpdateCh:
			d.checkSelfUpdate(ctx)
		case <-heartbeatTicker.C:
			d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceHeartbeat{SourceHeartbeat: &pb.SourceHeartbeat{}}})
		case <-d.ws.Reconnected():
			d.log.InfoContext(ctx, "websocket reconnected, re-announcing")
			d.announceOnline(ctx)
		}
	}
}

// announceOnline sends the sourceOnline event and full game state.
// Called on initial connect and after each reconnect.
func (d *Daemon) announceOnline(ctx context.Context) {
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOnline{SourceOnline: &pb.SourceOnline{
		Version:   d.cfg.Version,
		Platform:  runtime.GOOS + "-" + runtime.GOARCH,
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: timestamppb.Now(),
	}}})

	d.discoverGames(ctx)

	for gameID, gameCfg := range d.cfg.Games {
		if !gameCfg.Enabled {
			d.log.DebugContext(ctx, "skipping disabled game", slog.String("game_id", gameID))
			continue
		}
		d.log.InfoContext(ctx, "initializing game",
			slog.String("game_id", gameID),
			slog.String("save_path", gameCfg.SavePath),
		)
		if !d.ensurePluginReady(ctx, gameID) {
			continue
		}
		d.scanGame(ctx, gameID, gameCfg)
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
	d.log.InfoContext(ctx, "daemon update available", slog.String("new_version", info.Version))
	d.applyDaemonUpdate(ctx, info)
}

func (d *Daemon) applyDaemonUpdate(ctx context.Context, info *UpdateInfo) {
	if d.updater == nil {
		return
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceUpdateStarted{SourceUpdateStarted: &pb.SourceUpdateStarted{
		Version: info.Version,
	}}})
	if err := d.updater.Apply(ctx, info, d.cfg.BinaryPath); err != nil {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceUpdateFailed{SourceUpdateFailed: &pb.SourceUpdateFailed{
			Version: info.Version,
			Message: err.Error(),
		}}})
		return
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_SourceOffline{SourceOffline: &pb.SourceOffline{
		Timestamp: timestamppb.Now(),
	}}})
	d.exitFunc(0)
}

func (d *Daemon) checkPluginUpdates(ctx context.Context) {
	updated, err := d.plugins.CheckForUpdates(ctx)
	if err != nil {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginUpdateCheckFailed{PluginUpdateCheckFailed: &pb.PluginUpdateCheckFailed{
			Message: err.Error(),
		}}})
		return
	}
	for _, gameID := range updated {
		d.log.InfoContext(ctx, "plugin updated", slog.String("game_id", gameID))
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginUpdated{PluginUpdated: &pb.PluginUpdated{
			GameId: gameID,
		}}})
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
	d.log.DebugContext(ctx, "ensuring plugin ready", slog.String("game_id", gameID))
	if ensureErr := d.plugins.EnsurePlugin(ctx, gameID); ensureErr != nil {
		d.log.ErrorContext(
			ctx,
			"plugin download failed",
			slog.String("game_id", gameID),
			slog.String("error", ensureErr.Error()),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PluginDownloadFailed{PluginDownloadFailed: &pb.PluginDownloadFailed{
			GameId:  gameID,
			Message: ensureErr.Error(),
		}}})
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
		d.log.InfoContext(ctx, "game discovered",
			slog.String("game_id", gameID),
			slog.String("name", info.Name),
			slog.String("path", path),
			slog.Int("file_count", len(matching)),
		)
		discovered = append(discovered, DiscoveredGame{
			GameID:         gameID,
			Name:           info.Name,
			Path:           path,
			FileCount:      len(matching),
			FileExtensions: info.FileExtensions,
		})
	}

	pbGames := make([]*pb.DiscoveredGame, len(discovered))
	for i, g := range discovered {
		pbGames[i] = &pb.DiscoveredGame{
			GameId:         g.GameID,
			Name:           g.Name,
			Path:           g.Path,
			FileCount:      int32(g.FileCount),
			FileExtensions: g.FileExtensions,
		}
	}
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GamesDiscovered{GamesDiscovered: &pb.GamesDiscovered{
		Games: pbGames,
	}}})
}

func (d *Daemon) scanGame(
	ctx context.Context, gameID string, cfg GameConfig,
) {
	d.log.InfoContext(ctx, "scanning game directory", slog.String("game_id", gameID), slog.String("path", cfg.SavePath))
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ScanStarted{ScanStarted: &pb.ScanStarted{
		GameId: gameID,
		Path:   cfg.SavePath,
	}}})

	info, err := d.fs.Stat(cfg.SavePath)
	if err != nil || !info.IsDir() {
		d.log.WarnContext(
			ctx,
			"game directory not found",
			slog.String("game_id", gameID),
			slog.String("path", cfg.SavePath),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: []string{cfg.SavePath},
		}}})
		return
	}

	entries, err := d.fs.ReadDir(cfg.SavePath)
	if err != nil {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: []string{cfg.SavePath},
		}}})
		return
	}

	matchingFiles := d.filterByExtension(entries, cfg.FileExtensions)
	d.log.InfoContext(ctx, "save files found",
		slog.String("game_id", gameID),
		slog.Int("count", len(matchingFiles)),
		slog.String("path", cfg.SavePath),
	)

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ScanCompleted{ScanCompleted: &pb.ScanCompleted{
		GameId:     gameID,
		Path:       cfg.SavePath,
		FilesFound: int32(len(matchingFiles)),
		FileNames:  matchingFiles,
	}}})

	if len(matchingFiles) == 0 {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameNotFound{GameNotFound: &pb.GameNotFound{
			GameId:       gameID,
			PathsChecked: []string{cfg.SavePath},
		}}})
		return
	}

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_GameDetected{GameDetected: &pb.GameDetected{
		GameId:    gameID,
		Path:      cfg.SavePath,
		SaveCount: int32(len(matchingFiles)),
	}}})

	if watchErr := d.watcher.Add(cfg.SavePath); watchErr != nil {
		return
	}
	d.mu.Lock()
	d.watchedDirs[cfg.SavePath] = gameID
	d.mu.Unlock()

	d.log.InfoContext(
		ctx,
		"watching game",
		slog.String("game_id", gameID),
		slog.String("path", cfg.SavePath),
		slog.Int("file_count", len(matchingFiles)),
	)
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_Watching{Watching: &pb.Watching{
		GameId:         gameID,
		Path:           cfg.SavePath,
		FilesMonitored: int32(len(matchingFiles)),
	}}})

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
	d.log.DebugContext(ctx, "parsing save file", slog.String("game_id", gameID), slog.String("file_name", fileName))
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseStarted{ParseStarted: &pb.ParseStarted{
		GameId:   gameID,
		FileName: fileName,
	}}})

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

	state, err := d.runner.Run(ctx, gameID, saveBytes, onStatus)
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

	stateJSON, marshalErr := json.Marshal(state)
	if marshalErr != nil {
		d.log.ErrorContext(
			ctx,
			"failed to marshal state",
			slog.String("game_id", gameID),
			slog.String("error", marshalErr.Error()),
		)
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseFailed{ParseFailed: &pb.ParseFailed{
			GameId:    gameID,
			FileName:  fileName,
			ErrorType: pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR,
			Message:   fmt.Sprintf("marshal state: %v", marshalErr),
		}}})
		return
	}

	d.log.InfoContext(
		ctx,
		"parse completed",
		slog.String("game_id", gameID),
		slog.String("file_name", fileName),
		slog.String("summary", state.Summary),
		slog.Int("size_bytes", len(stateJSON)),
	)
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_ParseCompleted{ParseCompleted: &pb.ParseCompleted{
		GameId:        gameID,
		FileName:      fileName,
		Identity:      toProtoIdentity(state.Identity),
		Summary:       state.Summary,
		SectionsCount: int32(len(state.Sections)),
		SizeBytes:     int64(len(stateJSON)),
	}}})

	d.pushState(ctx, gameID, state, stateJSON)
}

func (d *Daemon) pushState(
	ctx context.Context, gameID string, state *GameState, stateJSON []byte,
) {
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PushStarted{PushStarted: &pb.PushStarted{
		GameId:    gameID,
		Summary:   state.Summary,
		SizeBytes: int64(len(stateJSON)),
	}}})

	parsedAt := time.Now().UTC()
	result, err := d.pushWithRetry(ctx, gameID, state.Summary, parsedAt, stateJSON)
	if err != nil {
		// pushWithRetry already emitted pushFailed for retryable errors.
		// For non-retryable errors, emit pushFailed here.
		if !isPushRetryable(err) {
			d.log.ErrorContext(
				ctx,
				"push failed permanently",
				slog.String("game_id", gameID),
				slog.String("error", err.Error()),
			)
			d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PushFailed{PushFailed: &pb.PushFailed{
				GameId:    gameID,
				Message:   err.Error(),
				WillRetry: false,
			}}})
		}
		return
	}

	d.log.InfoContext(ctx, "push completed", slog.String("game_id", gameID), slog.String("save_uuid", result.SaveUUID))
	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PushCompleted{PushCompleted: &pb.PushCompleted{
		GameId:            gameID,
		SaveUuid:          result.SaveUUID,
		Summary:           state.Summary,
		SnapshotSizeBytes: int64(len(stateJSON)),
		Identity:          toProtoIdentity(state.Identity),
	}}})
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
			shift := uint(min(attempt-1, maxBitShift)) //nolint:gosec // G115: attempt is bounded by maxAttempts
			delay := min(d.cfg.Retry.BaseDelay<<shift, d.cfg.Retry.MaxDelay)
			d.log.WarnContext(
				ctx,
				"push failed, retrying",
				slog.String("game_id", gameID),
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.String("error", lastErr.Error()),
			)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("push retry interrupted: %w", ctx.Err())
			case <-time.After(delay):
			}
			// Re-emit pushStarted on retry.
			d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PushStarted{PushStarted: &pb.PushStarted{
				GameId:    gameID,
				Summary:   summary,
				SizeBytes: int64(len(stateJSON)),
			}}})
		}

		d.log.DebugContext(
			ctx,
			"pushing save data",
			slog.String("game_id", gameID),
			slog.Int("attempt", attempt),
			slog.Int("size_bytes", len(stateJSON)),
		)
		result, pushErr := d.pusher.Push(ctx, gameID, stateJSON, parsedAt)
		if pushErr == nil {
			return result, nil
		}
		lastErr = pushErr

		if !isPushRetryable(pushErr) {
			return nil, fmt.Errorf("push: %w", pushErr)
		}

		willRetry := attempt < maxAttempts-1
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_PushFailed{PushFailed: &pb.PushFailed{
			GameId:    gameID,
			Message:   pushErr.Error(),
			WillRetry: willRetry,
		}}})
	}
	return nil, lastErr
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
			d.scanGame(ctx, cmd.RescanGame.GameId, gameCfg)
		}
	case *pb.Message_TestPath:
		d.handleTestPath(ctx, cmd.TestPath.GameId, cmd.TestPath.Path)
	case *pb.Message_DiscoverGames:
		d.discoverGames(ctx)
	case *pb.Message_SourceUpdateAvailable:
		info := &UpdateInfo{
			Version:      cmd.SourceUpdateAvailable.Version,
			URL:          cmd.SourceUpdateAvailable.Url,
			SignatureURL: cmd.SourceUpdateAvailable.SignatureUrl,
			SHA256:       cmd.SourceUpdateAvailable.Sha256,
		}
		d.applyDaemonUpdate(ctx, info)
	}
}

// configGameResult is the per-game result of applying a ConfigUpdate.
type configGameResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	ResolvedPath string `json:"resolvedPath"`
}

// buildGameResult checks if a resolved path is a valid directory.
func (d *Daemon) buildGameResult(resolvedPath string) configGameResult {
	info, err := d.fs.Stat(resolvedPath)
	if err != nil {
		return configGameResult{Error: fmt.Sprintf("path not found: %s", resolvedPath), ResolvedPath: resolvedPath}
	}
	if !info.IsDir() {
		return configGameResult{
			Error:        fmt.Sprintf("path is not a directory: %s", resolvedPath),
			ResolvedPath: resolvedPath,
		}
	}
	return configGameResult{Success: true, ResolvedPath: resolvedPath}
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
		}

		d.mu.Lock()
		oldCfg, existed := d.cfg.Games[gameID]
		d.cfg.Games[gameID] = gameCfg
		d.mu.Unlock()

		switch {
		case !newGame.Enabled:
			d.log.InfoContext(ctx, "game disabled", slog.String("game_id", gameID))
			if existed {
				d.unwatchGame(ctx, oldCfg.SavePath)
			}
			results[gameID] = configGameResult{Success: true}
		case !existed || !oldCfg.Enabled:
			d.log.InfoContext(
				ctx,
				"new game configured",
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
			d.scanGame(ctx, gameID, gameCfg)
			results[gameID] = d.buildGameResult(resolvedPath)
		case oldCfg.SavePath != resolvedPath:
			d.log.InfoContext(
				ctx,
				"game path changed",
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
			d.scanGame(ctx, gameID, gameCfg)
			results[gameID] = d.buildGameResult(resolvedPath)
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
	d.mu.Lock()
	_, ok := d.watchedDirs[savePath]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.watchedDirs, savePath)
	d.mu.Unlock()

	if removeErr := d.watcher.Remove(savePath); removeErr != nil {
		d.log.DebugContext(
			ctx,
			"watcher remove failed",
			slog.String("save_path", savePath),
			slog.String("error", removeErr.Error()),
		)
	}
}

func (d *Daemon) handleTestPath(ctx context.Context, gameID, path string) {
	info, err := d.fs.Stat(path)
	if err != nil || !info.IsDir() {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_TestPathResult{TestPathResult: &pb.TestPathResult{
			GameId: gameID,
			Path:   path,
		}}})
		return
	}

	entries, err := d.fs.ReadDir(path)
	if err != nil {
		d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_TestPathResult{TestPathResult: &pb.TestPathResult{
			GameId: gameID,
			Path:   path,
		}}})
		return
	}

	d.mu.RLock()
	gameCfg := d.cfg.Games[gameID]
	d.mu.RUnlock()
	fileNames := d.filterByExtension(entries, gameCfg.FileExtensions)

	d.sendMessage(ctx, &pb.Message{Payload: &pb.Message_TestPathResult{TestPathResult: &pb.TestPathResult{
		GameId:     gameID,
		Path:       path,
		Valid:      len(fileNames) > 0,
		FilesFound: int32(len(fileNames)),
		FileNames:  fileNames,
	}}})
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
		si.Extra, _ = structpb.NewStruct(id.Extra)
	}
	return si
}

