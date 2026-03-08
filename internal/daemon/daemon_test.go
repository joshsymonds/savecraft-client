package daemon

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Fakes ---

type fakeFS struct {
	files         map[string][]byte   // full path -> contents
	dirs          map[string][]string // dir path -> file names
	readFileCount int                 // number of ReadFile calls (for verifying bypass)
}

func (f *fakeFS) Stat(path string) (fs.FileInfo, error) {
	if _, ok := f.dirs[path]; ok {
		return &fakeFileInfo{name: filepath.Base(path), dir: true}, nil
	}
	if data, ok := f.files[path]; ok {
		return &fakeFileInfo{name: filepath.Base(path), size: int64(len(data))}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeFS) ReadDir(path string) ([]fs.DirEntry, error) {
	names, ok := f.dirs[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	entries := make([]fs.DirEntry, len(names))
	for i, name := range names {
		entries[i] = &fakeDirEntry{name: name}
	}
	return entries, nil
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	f.readFileCount++
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

type fakeFileInfo struct {
	name string
	size int64
	dir  bool
}

func (fi *fakeFileInfo) Name() string {
	return fi.name
}

func (fi *fakeFileInfo) Size() int64 {
	return fi.size
}

func (fi *fakeFileInfo) Mode() fs.FileMode {
	return 0o644
}

func (fi *fakeFileInfo) ModTime() time.Time {
	return time.Time{}
}

func (fi *fakeFileInfo) IsDir() bool {
	return fi.dir
}

func (fi *fakeFileInfo) Sys() any {
	return nil
}

type fakeDirEntry struct{ name string }

func (de *fakeDirEntry) Name() string {
	return de.name
}

func (de *fakeDirEntry) IsDir() bool {
	return false
}

func (de *fakeDirEntry) Type() fs.FileMode {
	return 0
}

func (de *fakeDirEntry) Info() (fs.FileInfo, error) {
	return &fakeFileInfo{name: de.name}, nil
}

type fakeWatcher struct {
	events  chan FileEvent
	added   []string
	removed []string
	mu      sync.Mutex
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{events: make(chan FileEvent, 10)}
}

func (w *fakeWatcher) Add(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.added = append(w.added, path)
	return nil
}

func (w *fakeWatcher) Remove(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed = append(w.removed, path)
	return nil
}

func (w *fakeWatcher) Events() <-chan FileEvent { return w.events }
func (w *fakeWatcher) Close() error             { return nil }

type fakeRunner struct {
	results    map[string]*GameState
	errors     map[string]error
	statusMsgs map[string][]string // gameID -> status messages to emit
	calls      []runCall
	mu         sync.Mutex
}

type runCall struct {
	GameID    string
	SaveBytes []byte
}

func (r *fakeRunner) Run(
	_ context.Context,
	gameID string,
	saveBytes []byte,
	onStatus func(string),
) (*GameState, error) {
	r.mu.Lock()
	r.calls = append(r.calls, runCall{GameID: gameID, SaveBytes: saveBytes})
	r.mu.Unlock()

	if msgs, ok := r.statusMsgs[gameID]; ok {
		for _, msg := range msgs {
			onStatus(msg)
		}
	}

	if err, ok := r.errors[gameID]; ok {
		return nil, err
	}
	if result, ok := r.results[gameID]; ok {
		return result, nil
	}
	return nil, fmt.Errorf("no result configured for game %s", gameID)
}

type fakeWSClient struct {
	messages    chan []byte
	reconnected chan struct{}
	sent        [][]byte
	connected   bool
	mu          sync.Mutex
}

func newFakeWSClient() *fakeWSClient {
	return &fakeWSClient{
		messages:    make(chan []byte, 10),
		reconnected: make(chan struct{}, 1),
	}
}

func (ws *fakeWSClient) Connect(_ context.Context) error {
	ws.connected = true
	return nil
}

func (ws *fakeWSClient) Send(msg []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	ws.sent = append(ws.sent, cp)
	return nil
}

func (ws *fakeWSClient) Messages() <-chan []byte      { return ws.messages }
func (ws *fakeWSClient) Reconnected() <-chan struct{} { return ws.reconnected }
func (ws *fakeWSClient) Close() error                 { return nil }
func (ws *fakeWSClient) Connected() bool              { return ws.connected }

// protoTypeName returns the oneof case name for a proto Message (e.g. "sourceOnline").
func protoTypeName(msg *pb.Message) string {
	switch msg.Payload.(type) {
	case *pb.Message_SourceOnline:
		return "sourceOnline"
	case *pb.Message_SourceOffline:
		return "sourceOffline"
	case *pb.Message_SourceHeartbeat:
		return "sourceHeartbeat"
	case *pb.Message_ScanStarted:
		return "scanStarted"
	case *pb.Message_ScanCompleted:
		return "scanCompleted"
	case *pb.Message_GameDetected:
		return "gameDetected"
	case *pb.Message_GameNotFound:
		return "gameNotFound"
	case *pb.Message_Watching:
		return "watching"
	case *pb.Message_GamesDiscovered:
		return "gamesDiscovered"
	case *pb.Message_ParseStarted:
		return "parseStarted"
	case *pb.Message_PluginStatus:
		return "pluginStatus"
	case *pb.Message_ParseCompleted:
		return "parseCompleted"
	case *pb.Message_ParseFailed:
		return "parseFailed"
	case *pb.Message_PushSave:
		return "pushSave"
	case *pb.Message_PushSaveResult:
		return "pushSaveResult"
	case *pb.Message_PluginUpdated:
		return "pluginUpdated"
	case *pb.Message_PluginUpdateCheckFailed:
		return "pluginUpdateCheckFailed"
	case *pb.Message_PluginDownloadFailed:
		return "pluginDownloadFailed"
	case *pb.Message_SourceUpdateStarted:
		return "sourceUpdateStarted"
	case *pb.Message_SourceUpdateFailed:
		return "sourceUpdateFailed"
	case *pb.Message_SourceUpdateAvailable:
		return "sourceUpdateAvailable"
	case *pb.Message_ConfigUpdate:
		return "configUpdate"
	case *pb.Message_ConfigResult:
		return "configResult"
	case *pb.Message_RescanGame:
		return "rescanGame"
	case *pb.Message_TestPath:
		return "testPath"
	case *pb.Message_TestPathResult:
		return "testPathResult"
	case *pb.Message_DiscoverGames:
		return "discoverGames"
	case *pb.Message_SourceState:
		return "sourceState"
	case *pb.Message_RefreshLinkCode:
		return "refreshLinkCode"
	case *pb.Message_UnlinkSource:
		return "unlinkSource"
	case *pb.Message_DeregisterSource:
		return "deregisterSource"
	default:
		return "unknown"
	}
}

func (ws *fakeWSClient) sentEventTypes() []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	var types []string
	for _, data := range ws.sent {
		var msg pb.Message
		if proto.Unmarshal(data, &msg) == nil {
			types = append(types, protoTypeName(&msg))
		}
	}
	return types
}

// sentProto returns the nth proto Message matching the given type name.
func (ws *fakeWSClient) sentProto(eventType string, index int) *pb.Message {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	count := 0
	for _, data := range ws.sent {
		var msg pb.Message
		if proto.Unmarshal(data, &msg) != nil {
			continue
		}
		if protoTypeName(&msg) != eventType {
			continue
		}
		if count == index {
			if cloned, ok := proto.Clone(&msg).(*pb.Message); ok {
				return cloned
			}
			return nil
		}
		count++
	}
	return nil
}

type fakePluginManager struct {
	ensured     []string
	ensureErr   map[string]error
	manifests   map[string]pluginmgr.PluginInfo
	manifestErr error
	mu          sync.Mutex
}

func (pm *fakePluginManager) EnsurePlugin(_ context.Context, gameID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensured = append(pm.ensured, gameID)
	if pm.ensureErr != nil {
		if err, ok := pm.ensureErr[gameID]; ok {
			return err
		}
	}
	return nil
}

func (pm *fakePluginManager) CheckForUpdates(_ context.Context) ([]string, error) {
	return nil, nil
}

func (pm *fakePluginManager) Manifests(_ context.Context) (map[string]pluginmgr.PluginInfo, error) {
	if pm.manifestErr != nil {
		return nil, pm.manifestErr
	}
	return pm.manifests, nil
}

type fakeUpdater struct {
	checkResult *UpdateInfo
	checkErr    error
	applyErr    error
	applyCalls  []applyCall
	mu          sync.Mutex
}

type applyCall struct {
	Info       *UpdateInfo
	BinaryPath string
}

func (u *fakeUpdater) Check(_ context.Context, _, _ string) (*UpdateInfo, error) {
	return u.checkResult, u.checkErr
}

func (u *fakeUpdater) Apply(_ context.Context, info *UpdateInfo, binaryPath string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.applyCalls = append(u.applyCalls, applyCall{Info: info, BinaryPath: binaryPath})
	return u.applyErr
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// --- Fixtures ---

func newD2RState() *GameState {
	return &GameState{
		Identity: Identity{
			SaveName: "Hammerdin",
			GameID:   "d2r",
			Extra:    map[string]any{"class": "Paladin", "level": float64(89)},
		},
		Summary: "Hammerdin, Level 89 Paladin",
		Sections: map[string]Section{
			"overview": {Description: "Character overview", Data: jsontext.Value(`{"level":89}`)},
		},
	}
}

func d2rConfig() Config {
	return Config{
		SourceID: "steam-deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s"}, Enabled: true},
		},
	}
}

func d2rFS() *fakeFS {
	return &fakeFS{
		dirs:  map[string][]string{"/saves/d2r": {"Hammerdin.d2s", "readme.txt"}},
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("fake save data")},
	}
}

func d2rRunner() *fakeRunner {
	return &fakeRunner{results: map[string]*GameState{"d2r": newD2RState()}}
}

func newStashState() *GameState {
	return &GameState{
		Identity: Identity{
			GameID: "d2r",
		},
		Summary: "Shared Stash (Softcore), 60 items, 0 gold",
		Sections: map[string]Section{
			"overview": {Description: "Shared stash overview", Data: jsontext.Value(`{"gold":0}`)},
		},
	}
}

// --- Tests: game-scoped identity ---

func TestGameScopedIdentity_OmitsSaveName(t *testing.T) {
	state := newStashState()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// saveName should not appear in JSON when empty.
	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(data, &raw); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}
	var identity map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(raw["identity"], &identity); unmarshalErr != nil {
		t.Fatalf("unmarshal identity: %v", unmarshalErr)
	}
	if _, hasCharName := identity["saveName"]; hasCharName {
		t.Error("game-scoped identity should not have saveName key")
	}
	if string(identity["gameId"]) != `"d2r"` {
		t.Errorf("gameId = %s, want \"d2r\"", identity["gameId"])
	}
}

func TestParseAndPush_GameScopedSave(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"d2r": newStashState()}}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/SharedStash.d2i": []byte("stash data")},
	}
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s", ".d2i"}, Enabled: true},
		},
	}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/SharedStash.d2i", "SharedStash.d2i", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushSave") {
		t.Error("missing pushSave")
	}

	// Identity in parseCompleted should have empty name for game-scoped saves.
	msg := ws.sentProto("parseCompleted", 0)
	if msg == nil {
		t.Fatal("missing parseCompleted")
	}
	pc := msg.GetParseCompleted()
	if pc.Identity == nil {
		t.Fatal("parseCompleted missing identity")
	}
	if pc.Identity.Name != "" {
		t.Error("game-scoped parseCompleted should not have saveName")
	}

	pushMsg := ws.sentProto("pushSave", 0)
	if pushMsg == nil {
		t.Fatal("missing pushSave")
	}
	ps := pushMsg.GetPushSave()
	if ps.Identity.Name != "" {
		t.Errorf("pushed saveName = %q, want empty", ps.Identity.Name)
	}
	if ps.GameId != "d2r" {
		t.Errorf("pushed gameId = %q, want d2r", ps.GameId)
	}
}

// --- Tests: scanGame ---

func TestScanGame_DetectsGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"])

	types := ws.sentEventTypes()
	for _, want := range []string{"scanStarted", "scanCompleted", "gameDetected", "watching", "pushSave"} {
		if !slices.Contains(types, want) {
			t.Errorf("missing %s event", want)
		}
	}

	msg := ws.sentProto("gameDetected", 0)
	if msg == nil {
		t.Fatal("missing gameDetected")
	}
	detected := msg.GetGameDetected()
	if detected.GameId != "d2r" {
		t.Errorf("gameDetected gameId = %v, want d2r", detected.GameId)
	}
	if detected.SaveCount != 1 {
		t.Errorf("gameDetected saveCount = %v, want 1", detected.SaveCount)
	}

	// Only .d2s matched, not .txt
	scMsg := ws.sentProto("scanCompleted", 0)
	if scMsg == nil {
		t.Fatal("missing scanCompleted")
	}
	completed := scMsg.GetScanCompleted()
	if completed.FilesFound != 1 {
		t.Errorf("scanCompleted filesFound = %v, want 1", completed.FilesFound)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if string(runner.calls[0].SaveBytes) != "fake save data" {
		t.Error("runner got wrong save bytes")
	}

	pushMsg := ws.sentProto("pushSave", 0)
	if pushMsg == nil {
		t.Fatal("missing pushSave")
	}
	ps := pushMsg.GetPushSave()
	if ps.Summary != "Hammerdin, Level 89 Paladin" {
		t.Error("pushSave got wrong summary")
	}
}

func TestScanGame_MissingDir(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"])

	types := ws.sentEventTypes()
	if !slices.Contains(types, "scanStarted") {
		t.Error("missing scanStarted")
	}
	if !slices.Contains(types, "gameNotFound") {
		t.Error("missing gameNotFound")
	}
	if slices.Contains(types, "gameDetected") {
		t.Error("unexpected gameDetected")
	}
}

func TestScanGame_NoMatchingFiles(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs: map[string][]string{"/saves/d2r": {"readme.txt", "notes.md"}},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"])

	types := ws.sentEventTypes()
	if !slices.Contains(types, "scanCompleted") {
		t.Error("missing scanCompleted")
	}
	if !slices.Contains(types, "gameNotFound") {
		t.Error("missing gameNotFound")
	}
	if slices.Contains(types, "gameDetected") {
		t.Error("unexpected gameDetected")
	}
}

// --- Tests: handleFileEvent ---

func TestHandleFileEvent_ParseAndPush(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("save data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileModify,
	})

	types := ws.sentEventTypes()
	for _, want := range []string{"parseStarted", "parseCompleted", "pushSave"} {
		if !slices.Contains(types, want) {
			t.Errorf("missing %s event", want)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
}

func TestHandleFileEvent_PreloadedDataBypassesReadFile(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := &fakeFS{
		// No files — ReadFile would fail if called.
		files: map[string][]byte{},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	preloaded := []byte("preloaded save data")
	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileModify,
		Data: preloaded,
	})

	if fsys.readFileCount != 0 {
		t.Errorf("ReadFile called %d times, want 0 (preloaded data should bypass)", fsys.readFileCount)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if string(runner.calls[0].SaveBytes) != string(preloaded) {
		t.Error("runner received wrong bytes, want preloaded data")
	}
}

func TestHandleFileEvent_IgnoresNonMatchingExtension(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)
	d.watchedDirs["/saves/d2r"] = "d2r"

	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/readme.txt",
		Op:   FileModify,
	})

	if len(ws.sentEventTypes()) != 0 {
		t.Error("should not send events for non-matching extension")
	}
}

func TestHandleFileEvent_IgnoresRemove(t *testing.T) {
	ws := newFakeWSClient()

	d := New(
		Config{}, &fakeFS{}, newFakeWatcher(), &fakeRunner{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)
	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileRemove,
	})

	if len(ws.sentEventTypes()) != 0 {
		t.Error("should not send events for file removal")
	}
}

func TestHandleFileEvent_IgnoresUnwatchedDir(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)
	// watchedDirs is empty -- no directories are being watched

	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileModify,
	})

	if len(ws.sentEventTypes()) != 0 {
		t.Error("should not send events for unwatched directory")
	}
}

// --- Tests: parseAndPush ---

func TestParseAndPush_PluginError(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{
		errors: map[string]error{
			"d2r": &PluginError{Type: "corrupt_file", Message: "bad header"},
		},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/bad.d2s": []byte("corrupt")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/bad.d2s", "bad.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "parseFailed") {
		t.Error("missing parseFailed")
	}
	if slices.Contains(types, "pushSave") {
		t.Error("unexpected pushSave after parse failure")
	}

	msg := ws.sentProto("parseFailed", 0)
	if msg == nil {
		t.Fatal("missing parseFailed")
	}
	failed := msg.GetParseFailed()
	// "corrupt_file" doesn't match proto enum names, so toParseErrorType falls back to PARSE_ERROR.
	if failed.ErrorType != pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR {
		t.Errorf("parseFailed errorType = %v, want PARSE_ERROR_TYPE_PARSE_ERROR", failed.ErrorType)
	}
}

func TestParseAndPush_FileReadError(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{files: map[string][]byte{}} // file doesn't exist
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/missing.d2s", "missing.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "parseFailed") {
		t.Error("missing parseFailed")
	}
	if slices.Contains(types, "pluginStatus") {
		t.Error("unexpected pluginStatus -- runner should not have been called")
	}
}

func TestPushState_SkipsNonObjectSections(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())

	state := &GameState{
		Identity: Identity{SaveName: "Test", GameID: "d2r"},
		Summary:  "test",
		Sections: map[string]Section{
			"valid_object": {Description: "An object section", Data: jsontext.Value(`{"key":"value"}`)},
			"bare_array":   {Description: "A bare array", Data: jsontext.Value(`[1,2,3]`)},
			"bare_string":  {Description: "A bare string", Data: jsontext.Value(`"hello"`)},
			"bare_number":  {Description: "A bare number", Data: jsontext.Value(`42`)},
		},
	}

	d.pushState(context.Background(), "d2r", "/saves/d2r/test.d2s", state)

	msg := ws.sentProto("pushSave", 0)
	if msg == nil {
		t.Fatal("missing pushSave message")
	}
	push := msg.GetPushSave()
	if len(push.Sections) != 1 {
		t.Fatalf("got %d sections, want 1 (only the valid object)", len(push.Sections))
	}
	if push.Sections[0].Name != "valid_object" {
		t.Errorf("got section %q, want %q", push.Sections[0].Name, "valid_object")
	}
}

func TestParseAndPush_ForwardsPluginStatus(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{
		results:    map[string]*GameState{"d2r": newD2RState()},
		statusMsgs: map[string][]string{"d2r": {"Decoding header", "Parsing inventory (247 items)"}},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)

	statusCount := 0
	for _, et := range ws.sentEventTypes() {
		if et == "pluginStatus" {
			statusCount++
		}
	}
	if statusCount != 2 {
		t.Errorf("got %d pluginStatus events, want 2", statusCount)
	}

	s1msg := ws.sentProto("pluginStatus", 0)
	if s1msg == nil {
		t.Fatal("missing pluginStatus 0")
	}
	s1 := s1msg.GetPluginStatus()
	if s1.Message != "Decoding header" {
		t.Errorf("status 0 message = %v", s1.Message)
	}
	s2msg := ws.sentProto("pluginStatus", 1)
	if s2msg == nil {
		t.Fatal("missing pluginStatus 1")
	}
	s2 := s2msg.GetPluginStatus()
	if s2.Message != "Parsing inventory (247 items)" {
		t.Errorf("status 1 message = %v", s2.Message)
	}
}

// --- Tests: handleCommand ---

func TestHandleCommand_RescanGame(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_RescanGame{RescanGame: &pb.RescanGame{
		GameId: "d2r",
	}}})
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "scanStarted") {
		t.Error("missing scanStarted from rescan")
	}
}

func TestHandleCommand_TestPath_Valid(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs: map[string][]string{"/custom/path": {"save1.d2s", "save2.d2s"}},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_TestPath{TestPath: &pb.TestPath{
		GameId: "d2r",
		Path:   "/custom/path",
	}}})
	d.handleCommand(context.Background(), cmd)

	msg := ws.sentProto("testPathResult", 0)
	if msg == nil {
		t.Fatal("missing testPathResult")
	}
	result := msg.GetTestPathResult()
	if result.Valid != true {
		t.Errorf("valid = %v, want true", result.Valid)
	}
	if result.FilesFound != 2 {
		t.Errorf("filesFound = %v, want 2", result.FilesFound)
	}
}

func TestHandleCommand_TestPath_Invalid(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_TestPath{TestPath: &pb.TestPath{
		GameId: "d2r",
		Path:   "/nonexistent",
	}}})
	d.handleCommand(context.Background(), cmd)

	msg := ws.sentProto("testPathResult", 0)
	if msg == nil {
		t.Fatal("missing testPathResult")
	}
	result := msg.GetTestPathResult()
	if result.Valid != false {
		t.Errorf("valid = %v, want false", result.Valid)
	}
}

// --- Tests: handleConfigUpdate ---

func TestConfigUpdate_AddsNewGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Should have scanned the new game.
	if !slices.Contains(ws.sentEventTypes(), "scanStarted") {
		t.Error("missing scanStarted for new game")
	}
	if !slices.Contains(ws.sentEventTypes(), "gameDetected") {
		t.Error("missing gameDetected for new game")
	}

	// Watcher should have added the save directory.
	watcher.mu.Lock()
	added := slices.Clone(watcher.added)
	watcher.mu.Unlock()
	if !slices.Contains(added, "/saves/d2r") {
		t.Errorf("watcher.added = %v, want /saves/d2r", added)
	}

	// Config should be updated.
	gameCfg, ok := d.cfg.Games["d2r"]
	if !ok {
		t.Fatal("d2r not in config after update")
	}
	if gameCfg.SavePath != "/saves/d2r" {
		t.Errorf("SavePath = %s", gameCfg.SavePath)
	}
}

func TestConfigUpdate_DisablesGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	cfg := d2rConfig()

	d := New(cfg, d2rFS(), watcher, d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        false,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Watcher should have removed the directory.
	watcher.mu.Lock()
	removed := slices.Clone(watcher.removed)
	watcher.mu.Unlock()
	if !slices.Contains(removed, "/saves/d2r") {
		t.Errorf("watcher.removed = %v, want /saves/d2r", removed)
	}

	// watchedDirs should be cleared.
	if _, ok := d.watchedDirs["/saves/d2r"]; ok {
		t.Error("watchedDirs still contains /saves/d2r")
	}
}

func TestConfigUpdate_RemovesGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	cfg := d2rConfig()

	d := New(cfg, d2rFS(), watcher, d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	// Send empty config -- d2r is no longer present.
	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Watcher should have removed the directory.
	watcher.mu.Lock()
	removed := slices.Clone(watcher.removed)
	watcher.mu.Unlock()
	if !slices.Contains(removed, "/saves/d2r") {
		t.Errorf("watcher.removed = %v, want /saves/d2r", removed)
	}

	// Game should be removed from config.
	if _, ok := d.cfg.Games["d2r"]; ok {
		t.Error("d2r still in config after removal")
	}
}

func TestConfigUpdate_ChangesPath(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := &fakeFS{
		dirs:  map[string][]string{"/new/path": {"Hero.d2s"}},
		files: map[string][]byte{"/new/path/Hero.d2s": []byte("save data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/new/path",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Should have removed old path.
	watcher.mu.Lock()
	removed := slices.Clone(watcher.removed)
	added := slices.Clone(watcher.added)
	watcher.mu.Unlock()
	if !slices.Contains(removed, "/saves/d2r") {
		t.Errorf("watcher.removed = %v, want /saves/d2r", removed)
	}

	// Should have added new path.
	if !slices.Contains(added, "/new/path") {
		t.Errorf("watcher.added = %v, want /new/path", added)
	}

	// Config should reflect new path.
	if d.cfg.Games["d2r"].SavePath != "/new/path" {
		t.Errorf("SavePath = %s, want /new/path", d.cfg.Games["d2r"].SavePath)
	}
}

func TestConfigUpdate_ReenablesGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s"}, Enabled: false},
		},
	}

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Should scan the re-enabled game.
	if !slices.Contains(ws.sentEventTypes(), "scanStarted") {
		t.Error("missing scanStarted for re-enabled game")
	}
}

// --- Tests: Run lifecycle ---

func TestRun_LifecycleEvents(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "steam-deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitFor(t, func() bool {
		return len(ws.sentEventTypes()) >= 1
	})

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	types := ws.sentEventTypes()
	if types[0] != "sourceOnline" {
		t.Errorf("first event = %v, want sourceOnline", types[0])
	}
	if types[len(types)-1] != "sourceOffline" {
		t.Errorf("last event = %v, want sourceOffline", types[len(types)-1])
	}

	msg := ws.sentProto("sourceOnline", 0)
	if msg == nil {
		t.Fatal("missing sourceOnline")
	}
	online := msg.GetSourceOnline()
	if online.Version != "0.1.0" {
		t.Errorf("sourceOnline version = %v", online.Version)
	}
}

func TestRun_FileEventTriggersParseAndPush(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("save data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "sourceOnline")
	})

	watcher.events <- FileEvent{Path: "/saves/d2r/Hammerdin.d2s", Op: FileModify}

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "pushSave")
	})

	runner.mu.Lock()
	runnerCalls := len(runner.calls)
	runner.mu.Unlock()
	if runnerCalls != 1 {
		t.Errorf("runner called %d times, want 1", runnerCalls)
	}

	cancel()
	<-done
}

func TestRun_WSCommandHandled(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for startup (scan + initial parse)
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "pushSave")
	})

	// Clear sent to isolate the rescan
	ws.mu.Lock()
	ws.sent = nil
	ws.mu.Unlock()

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_RescanGame{RescanGame: &pb.RescanGame{
		GameId: "d2r",
	}}})
	ws.messages <- cmd

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "scanStarted")
	})

	cancel()
	<-done
}

// --- Tests: PluginManager integration ---

func TestConfigUpdate_EnsurePluginFailed_SkipsGame(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, pm, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pluginDownloadFailed") {
		t.Error("missing pluginDownloadFailed event")
	}
	if slices.Contains(types, "scanStarted") {
		t.Error("should not scan when plugin download fails")
	}

	msg := ws.sentProto("pluginDownloadFailed", 0)
	if msg == nil {
		t.Fatal("missing pluginDownloadFailed")
	}
	failed := msg.GetPluginDownloadFailed()
	if failed.GameId != "d2r" {
		t.Errorf("pluginDownloadFailed gameId = %v, want d2r", failed.GameId)
	}
}

func TestRun_EnsurePluginFailed_SkipsGame(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("network error")},
	}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, pm, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "pluginDownloadFailed")
	})

	// Should NOT have scanned.
	if slices.Contains(ws.sentEventTypes(), "scanStarted") {
		t.Error("should not scan when EnsurePlugin fails at startup")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// --- Tests: zombie config removal on plugin failure ---

func TestConfigUpdate_NewGame_PluginFailure_RemovesFromConfig(t *testing.T) {
	ws := newFakeWSClient()
	fsys := d2rFS()
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}

	d := New(cfg, fsys, newFakeWatcher(), d2rRunner(), ws, pm, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Game should be removed from config after plugin failure.
	if _, ok := d.cfg.Games["d2r"]; ok {
		t.Error("d2r should be removed from config after plugin download failure")
	}

	if !slices.Contains(ws.sentEventTypes(), "pluginDownloadFailed") {
		t.Error("missing pluginDownloadFailed event")
	}
}

func TestConfigUpdate_PathChange_PluginFailure_RemovesFromConfig(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	fsys := &fakeFS{
		dirs:  map[string][]string{"/new/path": {"Hero.d2s"}},
		files: map[string][]byte{"/new/path/Hero.d2s": []byte("data")},
	}
	cfg := d2rConfig()

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}

	d := New(cfg, fsys, watcher, d2rRunner(), ws, pm, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/new/path",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	// Game should be removed from config after plugin failure on path change.
	if _, ok := d.cfg.Games["d2r"]; ok {
		t.Error("d2r should be removed from config after plugin download failure on path change")
	}

	// Old path should have been unwatched.
	watcher.mu.Lock()
	removed := slices.Clone(watcher.removed)
	watcher.mu.Unlock()
	if !slices.Contains(removed, "/saves/d2r") {
		t.Errorf("watcher.removed = %v, want /saves/d2r", removed)
	}
}

// --- Tests: ConfigResult ---

// configResultGame extracts a per-game GameConfigResult from a configResult event.
func configResultGame(t *testing.T, ws *fakeWSClient, gameID string) *pb.GameConfigResult {
	t.Helper()
	msg := ws.sentProto("configResult", 0)
	if msg == nil {
		t.Fatal("missing configResult event")
	}
	cr := msg.GetConfigResult()
	if cr == nil {
		t.Fatal("configResult payload is nil")
	}
	game, ok := cr.Results[gameID]
	if !ok {
		t.Fatalf("%s result not found in configResult", gameID)
	}
	return game
}

func TestConfigResult_ValidPath(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg, d2rFS(), newFakeWatcher(), d2rRunner(),
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d2rResult := configResultGame(t, ws, "d2r")
	if d2rResult.Success != true {
		t.Errorf("success = %v, want true", d2rResult.Success)
	}
	if d2rResult.ResolvedPath != "/saves/d2r" {
		t.Errorf("resolvedPath = %v, want /saves/d2r", d2rResult.ResolvedPath)
	}
	if d2rResult.Error != "" {
		t.Errorf("error = %v, want empty", d2rResult.Error)
	}
}

func TestConfigResult_InvalidPath(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg, fsys, newFakeWatcher(), &fakeRunner{},
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/nonexistent/path",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d2rResult := configResultGame(t, ws, "d2r")
	if d2rResult.Success != false {
		t.Errorf("success = %v, want false", d2rResult.Success)
	}
	if d2rResult.Error == "" {
		t.Error("error should be non-empty for invalid path")
	}
}

func TestConfigResult_DisabledGame(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(
		cfg, d2rFS(), newFakeWatcher(), d2rRunner(),
		ws, &fakePluginManager{}, nil, testLogger(),
	)
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        false,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d2rResult := configResultGame(t, ws, "d2r")
	if d2rResult.Success != true {
		t.Errorf("success = %v, want true for disabled game", d2rResult.Success)
	}
}

func TestConfigResult_MultipleGames(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs:  map[string][]string{"/saves/d2r": {"Hammerdin.d2s"}},
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("fake")},
	}
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg, fsys, newFakeWatcher(), d2rRunner(),
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
			"sdv": {
				SavePath:       "/nonexistent/sdv",
				Enabled:        true,
				FileExtensions: []string{".xml"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d2rResult := configResultGame(t, ws, "d2r")
	if d2rResult.Success != true {
		t.Errorf("d2r success = %v, want true", d2rResult.Success)
	}

	sdvResult := configResultGame(t, ws, "sdv")
	if sdvResult.Success != false {
		t.Errorf("sdv success = %v, want false", sdvResult.Success)
	}
}

func TestConfigResult_ExpandsTildePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	expandedPath := filepath.Join(home, "saves", "d2r")

	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs:  map[string][]string{expandedPath: {"Hammerdin.d2s"}},
		files: map[string][]byte{filepath.Join(expandedPath, "Hammerdin.d2s"): []byte("fake")},
	}
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg, fsys, newFakeWatcher(), d2rRunner(),
		ws, &fakePluginManager{}, nil, testLogger(),
	)

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {
				SavePath:       "~/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d2rResult := configResultGame(t, ws, "d2r")
	if d2rResult.Success != true {
		t.Errorf("success = %v, want true", d2rResult.Success)
	}
	if d2rResult.ResolvedPath != expandedPath {
		t.Errorf("resolvedPath = %v, want %s", d2rResult.ResolvedPath, expandedPath)
	}

	d.mu.RLock()
	stored := d.cfg.Games["d2r"]
	d.mu.RUnlock()
	if stored.SavePath != expandedPath {
		t.Errorf("stored SavePath = %s, want %s", stored.SavePath, expandedPath)
	}
}

// --- Tests: discoverGames ---

func TestDiscoverGames_FindsGame(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs:  map[string][]string{"/home/user/saves/d2r": {"Hammerdin.d2s", "readme.txt"}},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {
				GameID:         "d2r",
				Name:           "Diablo II: Resurrected",
				DefaultPaths:   map[string]string{runtime.GOOS: "/home/user/saves/d2r"},
				FileExtensions: []string{".d2s"},
			},
		},
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, fsys,
		newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	msg := ws.sentProto("gamesDiscovered", 0)
	if msg == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	gd := msg.GetGamesDiscovered()
	if len(gd.Games) != 1 {
		t.Fatalf("games count = %d, want 1", len(gd.Games))
	}

	game := gd.Games[0]
	if game.GameId != "d2r" {
		t.Errorf("gameId = %v, want d2r", game.GameId)
	}
	if game.Name != "Diablo II: Resurrected" {
		t.Errorf("name = %v", game.Name)
	}
	if game.Path != "/home/user/saves/d2r" {
		t.Errorf("path = %v", game.Path)
	}
	if game.FileCount != 1 {
		t.Errorf("fileCount = %v, want 1", game.FileCount)
	}
}

func TestDiscoverGames_NilPluginManager(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{Games: map[string]GameConfig{}}
	d := New(
		cfg, &fakeFS{}, newFakeWatcher(),
		&fakeRunner{}, ws, nil, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	if len(ws.sentEventTypes()) != 0 {
		t.Error("should not send events with nil plugin manager")
	}
}

func TestDiscoverGames_NoMatchingPaths(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs:  map[string][]string{},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {
				GameID:         "d2r",
				Name:           "Diablo II: Resurrected",
				DefaultPaths:   map[string]string{runtime.GOOS: "/nonexistent/path"},
				FileExtensions: []string{".d2s"},
			},
		},
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, fsys,
		newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	msg := ws.sentProto("gamesDiscovered", 0)
	if msg == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	gd := msg.GetGamesDiscovered()
	// games should be nil/empty since path doesn't exist.
	if len(gd.Games) != 0 {
		t.Errorf("games = %v, want empty", gd.Games)
	}
}

func TestDiscoverGames_MixedResults(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs: map[string][]string{
			"/home/user/saves/d2r": {"Hammerdin.d2s"},
		},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {
				GameID:         "d2r",
				Name:           "Diablo II: Resurrected",
				DefaultPaths:   map[string]string{runtime.GOOS: "/home/user/saves/d2r"},
				FileExtensions: []string{".d2s"},
			},
			"poe": {
				GameID:         "poe",
				Name:           "Path of Exile",
				DefaultPaths:   map[string]string{runtime.GOOS: "/nonexistent/poe"},
				FileExtensions: []string{".filter"},
			},
		},
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, fsys,
		newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	msg := ws.sentProto("gamesDiscovered", 0)
	if msg == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	gd := msg.GetGamesDiscovered()
	if len(gd.Games) != 1 {
		t.Fatalf("games len = %d, want 1 (only d2r found)", len(gd.Games))
	}

	if gd.Games[0].GameId != "d2r" {
		t.Errorf("found game = %v, want d2r", gd.Games[0].GameId)
	}
}

func TestDiscoverGames_ManifestError(t *testing.T) {
	ws := newFakeWSClient()

	pm := &fakePluginManager{
		manifestErr: fmt.Errorf("network error"),
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, &fakeFS{},
		newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	// Should not send any event when manifest fetch fails.
	if len(ws.sentEventTypes()) != 0 {
		t.Errorf("sent events = %v, want none on manifest error", ws.sentEventTypes())
	}
}

func TestHandleCommand_DiscoverGames(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs:  map[string][]string{"/saves/d2r": {"Hero.d2s"}},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {
				GameID:         "d2r",
				Name:           "Diablo II: Resurrected",
				DefaultPaths:   map[string]string{runtime.GOOS: "/saves/d2r"},
				FileExtensions: []string{".d2s"},
			},
		},
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, fsys,
		newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger(),
	)

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_DiscoverGames{DiscoverGames: &pb.DiscoverGames{}}})
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "gamesDiscovered") {
		t.Error("missing gamesDiscovered event from command")
	}
}

// --- Tests: daemon self-update ---

func TestHandleCommand_DaemonUpdateAvailable(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	cfg := Config{
		SourceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	cmd, _ := proto.Marshal(
		&pb.Message{Payload: &pb.Message_SourceUpdateAvailable{SourceUpdateAvailable: &pb.SourceUpdateAvailable{
			Version:      "0.2.0",
			Url:          "https://example.com/daemon",
			SignatureUrl: "https://example.com/daemon.sig",
			Sha256:       "abc123",
		}}},
	)
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("missing sourceUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "sourceOffline") {
		t.Error("missing sourceOffline after successful update")
	}
	if exitCode != 0 {
		t.Errorf("exitFunc called with %d, want 0", exitCode)
	}

	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 1 {
		t.Fatalf("updater.Apply called %d times, want 1", calls)
	}
	if updater.applyCalls[0].Info.Version != "0.2.0" {
		t.Errorf("version = %s, want 0.2.0", updater.applyCalls[0].Info.Version)
	}
	if updater.applyCalls[0].BinaryPath != "/usr/local/bin/savecraft-daemon" {
		t.Errorf("binaryPath = %s", updater.applyCalls[0].BinaryPath)
	}
}

func TestHandleCommand_DaemonUpdateFailed(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{applyErr: fmt.Errorf("disk full")}
	cfg := Config{
		SourceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	cmd, _ := proto.Marshal(
		&pb.Message{Payload: &pb.Message_SourceUpdateAvailable{SourceUpdateAvailable: &pb.SourceUpdateAvailable{
			Version:      "0.2.0",
			Url:          "https://example.com/daemon",
			SignatureUrl: "https://example.com/daemon.sig",
			Sha256:       "abc123",
		}}},
	)
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("missing sourceUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "sourceUpdateFailed") {
		t.Error("missing sourceUpdateFailed event")
	}

	msg := ws.sentProto("sourceUpdateFailed", 0)
	if msg == nil {
		t.Fatal("missing sourceUpdateFailed")
	}
	failed := msg.GetSourceUpdateFailed()
	if failed.Message != "disk full" {
		t.Errorf("message = %v, want 'disk full'", failed.Message)
	}
}

func TestHandleCommand_DaemonUpdateAvailable_NilUpdater(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	cmd, _ := proto.Marshal(
		&pb.Message{Payload: &pb.Message_SourceUpdateAvailable{SourceUpdateAvailable: &pb.SourceUpdateAvailable{
			Version: "0.2.0",
			Url:     "https://example.com/daemon",
			Sha256:  "abc123",
		}}},
	)
	d.handleCommand(context.Background(), cmd)

	// Should not crash, should not send any update events
	if slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("should not start update with nil updater")
	}
}

func TestCheckSelfUpdate_TriggersApply(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{
		checkResult: &UpdateInfo{
			Version:      "0.3.0",
			URL:          "https://example.com/daemon",
			SignatureURL: "https://example.com/daemon.sig",
			SHA256:       "deadbeef",
		},
	}
	cfg := Config{
		SourceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	d.checkSelfUpdate(context.Background())

	if !slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("missing sourceUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "sourceOffline") {
		t.Error("missing sourceOffline after successful update")
	}
	if exitCode != 0 {
		t.Errorf("exitFunc called with %d, want 0", exitCode)
	}

	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 1 {
		t.Fatalf("updater.Apply called %d times, want 1", calls)
	}
	if updater.applyCalls[0].Info.Version != "0.3.0" {
		t.Errorf("version = %s, want 0.3.0", updater.applyCalls[0].Info.Version)
	}
}

func TestCheckSelfUpdate_NilResult(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{checkResult: nil}
	cfg := Config{
		SourceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	d.checkSelfUpdate(context.Background())

	if slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("should not start update when Check returns nil")
	}
}

func TestCheckSelfUpdate_CheckError(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{checkErr: fmt.Errorf("network error")}
	cfg := Config{
		SourceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	d.checkSelfUpdate(context.Background())

	if slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("should not start update when Check returns error")
	}
}

func TestCheckSelfUpdate_NilUpdater(_ *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	// Should not panic
	d.checkSelfUpdate(context.Background())
}

func TestSendEvent_HeartbeatWireFormat(t *testing.T) {
	// Verify sourceHeartbeat serializes as {"sourceHeartbeat":{}} (not null).
	// The hub's Message.fromJSON uses isSet() which returns false for null,
	// so the empty object is critical for the heartbeat to be recognized.
	ws := newFakeWSClient()
	d := New(
		Config{SourceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}},
		&fakeFS{}, newFakeWatcher(), &fakeRunner{},
		ws, nil, nil, testLogger(),
	)

	d.sendMessage(
		context.Background(),
		&pb.Message{Payload: &pb.Message_SourceHeartbeat{SourceHeartbeat: &pb.SourceHeartbeat{}}},
	)

	ws.mu.Lock()
	defer ws.mu.Unlock()
	if len(ws.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(ws.sent))
	}

	var msg pb.Message
	if err := proto.Unmarshal(ws.sent[0], &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	hb := msg.GetSourceHeartbeat()
	ok := hb != nil
	if !ok {
		t.Fatal("missing sourceHeartbeat payload")
	}
}

func TestRun_ReconnectReannounces(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				Enabled:        true,
				FileExtensions: []string{".d2s"},
			},
		},
	}
	fsys := &fakeFS{
		dirs:  map[string][]string{"/saves/d2r": {"test.d2s"}},
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{
		results: map[string]*GameState{"d2r": newD2RState()},
	}, ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for initial sourceOnline.
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "sourceOnline")
	})

	// Count initial sourceOnline events.
	initialCount := 0
	for _, et := range ws.sentEventTypes() {
		if et == "sourceOnline" {
			initialCount++
		}
	}

	// Simulate reconnect.
	ws.reconnected <- struct{}{}

	// Wait for second sourceOnline.
	waitFor(t, func() bool {
		count := 0
		for _, et := range ws.sentEventTypes() {
			if et == "sourceOnline" {
				count++
			}
		}
		return count > initialCount
	})

	// Verify re-announced after reconnect.
	// The second sourceOnline should have version and platform but no sourceId.
	onlineMsg := ws.sentProto("sourceOnline", 1)
	if onlineMsg == nil {
		t.Fatal("second sourceOnline event not found")
	}
	online := onlineMsg.GetSourceOnline()
	if online.Version != "0.1.0" {
		t.Errorf("reconnect sourceOnline version = %v, want 0.1.0", online.Version)
	}

	// Verify gamesDiscovered and watching are re-sent on reconnect.
	// pushSave is correctly skipped when data hasn't changed (hash dedup).
	eventTypes := ws.sentEventTypes()
	for _, required := range []string{"gamesDiscovered", "watching"} {
		count := 0
		for _, et := range eventTypes {
			if et == required {
				count++
			}
		}
		if count < 2 {
			t.Errorf("%s sent %d times, want >= 2 (initial + reconnect)", required, count)
		}
	}
	// pushSave should only be sent once — the reconnect parse produces
	// identical output, so the hash dedup skips the second push.
	pushCount := countEventType(ws, "pushSave")
	if pushCount != 1 {
		t.Errorf("pushSave sent %d times, want 1 (dedup should skip reconnect push)", pushCount)
	}

	cancel()
	<-done
}

// --- Tests: link state ---

func TestHandleSourceLinked_SetsLinkedAndCallsBack(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	var called bool
	d.SetLinkCallbacks(LinkCallbacks{
		OnLinked: func() { called = true },
	})
	d.SetInitialLinkCode("123456", time.Now().Add(20*time.Minute))

	msg := &pb.Message{Payload: &pb.Message_SourceLinked{SourceLinked: &pb.SourceLinked{}}}
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	d.handleCommand(context.Background(), data)

	if !d.linked {
		t.Error("expected linked=true after SourceLinked")
	}
	if d.linkCode != "" {
		t.Errorf("expected linkCode cleared, got %q", d.linkCode)
	}
	if !called {
		t.Error("expected OnLinked callback to be called")
	}
}

func TestHandleRefreshLinkCodeResult_UpdatesCodeAndCallsBack(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	var gotCode string
	var gotExpiry time.Time
	d.SetLinkCallbacks(LinkCallbacks{
		OnLinkCode: func(code string, expiresAt time.Time) {
			gotCode = code
			gotExpiry = expiresAt
		},
	})

	expiry := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	msg := &pb.Message{Payload: &pb.Message_RefreshLinkCodeResult{RefreshLinkCodeResult: &pb.RefreshLinkCodeResult{
		LinkCode:  "654321",
		ExpiresAt: timestamppb.New(expiry),
	}}}
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	d.handleCommand(context.Background(), data)

	if d.linkCode != "654321" {
		t.Errorf("linkCode = %q, want 654321", d.linkCode)
	}
	if gotCode != "654321" {
		t.Errorf("callback code = %q, want 654321", gotCode)
	}
	if !gotExpiry.Equal(expiry) {
		t.Errorf("callback expiry = %v, want %v", gotExpiry, expiry)
	}
}

func TestRefreshLinkCodeResult_DeliversToPendingChannel(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	expiry := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	msg := &pb.Message{Payload: &pb.Message_RefreshLinkCodeResult{RefreshLinkCodeResult: &pb.RefreshLinkCodeResult{
		LinkCode:  "111111",
		ExpiresAt: timestamppb.New(expiry),
	}}}
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	d.handleCommand(context.Background(), data)

	select {
	case result := <-d.pendingLinkCode:
		if result.Code != "111111" {
			t.Errorf("pending code = %q, want 111111", result.Code)
		}
	default:
		t.Error("expected result on pendingLinkCode channel")
	}
}

func TestMaybeRefreshLinkCode_SendsWhenNearExpiry(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	d.linkExpiry = time.Now().Add(30 * time.Second)

	d.maybeRefreshLinkCode(context.Background())

	types := ws.sentEventTypes()
	if !slices.Contains(types, "refreshLinkCode") {
		t.Errorf("expected refreshLinkCode sent, got %v", types)
	}
}

func TestMaybeRefreshLinkCode_SkipsWhenLinked(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	d.linked = true
	d.linkExpiry = time.Now().Add(30 * time.Second)

	d.maybeRefreshLinkCode(context.Background())

	types := ws.sentEventTypes()
	if slices.Contains(types, "refreshLinkCode") {
		t.Error("should not send refreshLinkCode when linked")
	}
}

func TestMaybeRefreshLinkCode_SkipsWhenFarFromExpiry(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	d.linkExpiry = time.Now().Add(10 * time.Minute)

	d.maybeRefreshLinkCode(context.Background())

	types := ws.sentEventTypes()
	if slices.Contains(types, "refreshLinkCode") {
		t.Error("should not send refreshLinkCode when far from expiry")
	}
}

func TestRequestUnlink_SendsAndBlocksForResult(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	ws.connected = true

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	expiry := time.Now().Add(20 * time.Minute).Truncate(time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		d.pendingLinkCode <- linkCodeResult{Code: "999999", ExpiresAt: expiry}
	}()

	code, gotExpiry, err := d.RequestUnlink(ctx)
	if err != nil {
		t.Fatalf("RequestUnlink: %v", err)
	}
	if code != "999999" {
		t.Errorf("code = %q, want 999999", code)
	}
	if !gotExpiry.Equal(expiry) {
		t.Errorf("expiry = %v, want %v", gotExpiry, expiry)
	}

	types := ws.sentEventTypes()
	if !slices.Contains(types, "unlinkSource") {
		t.Errorf("expected unlinkSource sent, got %v", types)
	}
}

func TestRequestUnlink_TimesOut(t *testing.T) {
	ws := newFakeWSClient()
	d := New(d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	ws.connected = true

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// No goroutine delivers to pendingLinkCode — context should expire.
	_, _, err := d.RequestUnlink(ctx)
	if err == nil {
		t.Fatal("expected error from RequestUnlink with expired context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestSetInitialLinkCode(t *testing.T) {
	d := New(
		d2rConfig(), d2rFS(), newFakeWatcher(), d2rRunner(),
		newFakeWSClient(), &fakePluginManager{}, nil, testLogger(),
	)

	expiry := time.Now().Add(20 * time.Minute)
	d.SetInitialLinkCode("ABCDEF", expiry)

	if d.linkCode != "ABCDEF" {
		t.Errorf("linkCode = %q, want ABCDEF", d.linkCode)
	}
	if !d.linkExpiry.Equal(expiry) {
		t.Errorf("linkExpiry = %v, want %v", d.linkExpiry, expiry)
	}
}

// --- Tests: PushSave output hash dedup ---

func TestParseAndPush_FirstParseAlwaysPushes(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushSave") {
		t.Error("first parse should always produce pushSave")
	}
}

func TestParseAndPush_SkipsPushWhenOutputUnchanged(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse — should push.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)

	pushCount1 := countEventType(ws, "pushSave")
	if pushCount1 != 1 {
		t.Fatalf("after first parse: pushSave count = %d, want 1", pushCount1)
	}

	// Second parse with identical output — should skip push.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)

	pushCount2 := countEventType(ws, "pushSave")
	if pushCount2 != 1 {
		t.Errorf("after second identical parse: pushSave count = %d, want 1 (should skip)", pushCount2)
	}

	// parseStarted still fires (we still parse, just skip the push).
	parseStartedCount := countEventType(ws, "parseStarted")
	if parseStartedCount != 2 {
		t.Errorf("parseStarted count = %d, want 2", parseStartedCount)
	}
}

func TestParseAndPush_PushesWhenOutputChanges(t *testing.T) {
	ws := newFakeWSClient()
	state1 := newD2RState()
	state2 := &GameState{
		Identity: Identity{
			SaveName: "Hammerdin",
			GameID:   "d2r",
			Extra:    map[string]any{"class": "Paladin", "level": float64(90)},
		},
		Summary: "Hammerdin, Level 90 Paladin",
		Sections: map[string]Section{
			"overview": {Description: "Character overview", Data: jsontext.Value(`{"level":90}`)},
		},
	}
	callCount := 0
	runner := &fakeRunner{
		results: map[string]*GameState{"d2r": state1},
	}
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)
	callCount++

	// Change the runner output to simulate leveling up.
	runner.mu.Lock()
	runner.results["d2r"] = state2
	runner.mu.Unlock()

	// Second parse with different output — should push.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)
	callCount++

	pushCount := countEventType(ws, "pushSave")
	if pushCount != 2 {
		t.Errorf("pushSave count = %d, want 2 (both should push since output changed)", pushCount)
	}
	_ = callCount
}

func TestParseAndPush_HashUpdatedOnlyAfterSuccessfulPush(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse — should push and cache hash.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil)

	if len(d.lastPushedHash) != 1 {
		t.Fatalf("lastPushedHash has %d entries, want 1", len(d.lastPushedHash))
	}
	hash, ok := d.lastPushedHash["/saves/d2r/Hammerdin.d2s"]
	if !ok {
		t.Fatal("lastPushedHash missing entry for /saves/d2r/Hammerdin.d2s")
	}
	if hash == [32]byte{} {
		t.Error("lastPushedHash should not be zero")
	}
}

// countEventType counts how many messages of the given type were sent.
func countEventType(ws *fakeWSClient, eventType string) int {
	count := 0
	for _, t := range ws.sentEventTypes() {
		if t == eventType {
			count++
		}
	}
	return count
}
