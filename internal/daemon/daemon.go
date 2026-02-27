// Package daemon coordinates file watching, plugin execution, and server communication.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
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
	CharacterName string         `json:"characterName,omitempty"`
	GameID        string         `json:"gameId"`
	Extra         map[string]any `json:"extra,omitempty"`
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

// --- Events and results ---

// FileEvent represents a filesystem change notification.
type FileEvent struct {
	Path string
	Op   FileOp
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

// Config holds all daemon configuration.
type Config struct {
	ServerURL string
	AuthToken string `json:"-"`
	DeviceID  string
	Version   string
	Games     map[string]GameConfig
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
	Run(ctx context.Context, gameID string, saveBytes []byte, onStatus func(string)) (*GameState, error)
}

// PushClient pushes parsed game state to the server.
type PushClient interface {
	Push(ctx context.Context, gameID string, state *GameState, parsedAt time.Time) (*PushResult, error)
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

// --- Daemon ---

// Daemon coordinates file watching, plugin execution, and server communication.
type Daemon struct {
	cfg     Config
	fs      FS
	watcher Watcher
	runner  Runner
	pusher  PushClient
	ws      WSClient

	// Maps watched directory → game ID.
	watchedDirs map[string]string
}

// New creates a Daemon with the given dependencies.
func New(cfg Config, fsys FS, watcher Watcher, runner Runner, pusher PushClient, ws WSClient) *Daemon {
	return &Daemon{
		cfg:         cfg,
		fs:          fsys,
		watcher:     watcher,
		runner:      runner,
		pusher:      pusher,
		ws:          ws,
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
	})

	for gameID, gameCfg := range d.cfg.Games {
		if !gameCfg.Enabled {
			continue
		}
		d.scanGame(ctx, gameID, gameCfg)
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
		}
	}
}

func (d *Daemon) scanGame(ctx context.Context, gameID string, cfg GameConfig) {
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
		d.parseAndPush(ctx, gameID, fullPath, fileName)
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
	d.parseAndPush(ctx, gameID, ev.Path, fileName)
}

func (d *Daemon) parseAndPush(ctx context.Context, gameID, fullPath, fileName string) {
	d.sendEvent("parseStarted", map[string]any{
		"gameId":   gameID,
		"fileName": fileName,
	})

	saveBytes, err := d.fs.ReadFile(fullPath)
	if err != nil {
		d.sendEvent("parseFailed", map[string]any{
			"gameId":    gameID,
			"fileName":  fileName,
			"errorType": "PARSE_ERROR_TYPE_PARSE_ERROR",
			"message":   fmt.Sprintf("read file: %v", err),
		})
		return
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
		var pluginErr *PluginError
		if errors.As(err, &pluginErr) {
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
	result, err := d.pusher.Push(ctx, gameID, state, parsedAt)
	if err != nil {
		d.sendEvent("pushFailed", map[string]any{
			"gameId":    gameID,
			"message":   err.Error(),
			"willRetry": false,
		})
		return
	}

	d.sendEvent("pushCompleted", map[string]any{
		"gameId":            gameID,
		"saveUuid":          result.SaveUUID,
		"summary":           state.Summary,
		"snapshotSizeBytes": len(stateJSON),
	})
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
	}
}

func (d *Daemon) handleConfigUpdate(ctx context.Context, payload json.RawMessage) {
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

	newGameIDs := make(map[string]bool, len(update.Games))
	for gameID := range update.Games {
		newGameIDs[gameID] = true
	}

	// Remove games that are no longer in the config.
	for gameID, oldCfg := range d.cfg.Games {
		if !newGameIDs[gameID] {
			d.unwatchGame(oldCfg.SavePath)
			delete(d.cfg.Games, gameID)
		}
	}

	// Add or update games.
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
			// Disable: stop watching.
			if existed {
				d.unwatchGame(oldCfg.SavePath)
			}
		case !existed || !oldCfg.Enabled:
			// New game or re-enabled: scan it.
			d.scanGame(ctx, gameID, gameCfg)
		case oldCfg.SavePath != newGame.SavePath:
			// Path changed: remove old watch, scan new path.
			d.unwatchGame(oldCfg.SavePath)
			d.scanGame(ctx, gameID, gameCfg)
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

func (d *Daemon) filterByExtension(entries []fs.DirEntry, extensions []string) []string {
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

func parseMessage(data []byte) (string, json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", nil, fmt.Errorf("unmarshal message envelope: %w", err)
	}
	for key, val := range envelope {
		return key, val, nil
	}
	return "", nil, fmt.Errorf("empty message")
}
