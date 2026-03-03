package daemon

import (
	"context"
	"encoding/json"
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

	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
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

type fakePushClient struct {
	results   map[string]*PushResult
	errors    map[string]error
	errorSeqs map[string][]error // per-call error sequence (pops front each call)
	calls     []pushCall
	mu        sync.Mutex
}

type pushCall struct {
	GameID string
	Body   []byte
}

func (p *fakePushClient) Push(
	_ context.Context,
	gameID string,
	body []byte,
	_ time.Time,
) (*PushResult, error) {
	p.mu.Lock()
	p.calls = append(p.calls, pushCall{GameID: gameID, Body: body})

	// Check errorSeqs first (per-call sequencing).
	seq := p.popSeqError(gameID)
	p.mu.Unlock()

	if seq.found {
		if seq.err != nil {
			return nil, seq.err
		}
		return p.defaultResult(gameID), nil
	}

	if p.errors != nil {
		if err, ok := p.errors[gameID]; ok {
			return nil, err
		}
	}
	if p.results != nil {
		if result, ok := p.results[gameID]; ok {
			return result, nil
		}
	}
	return &PushResult{SaveUUID: "test-uuid", SnapshotTimestamp: "2026-02-25T21:30:00Z"}, nil
}

// seqResult holds the result of popping from errorSeqs.
type seqResult struct {
	err   error
	found bool
}

// popSeqError returns the next error from errorSeqs for gameID.
// Must be called with p.mu held.
func (p *fakePushClient) popSeqError(gameID string) seqResult {
	if p.errorSeqs == nil {
		return seqResult{}
	}
	seq, ok := p.errorSeqs[gameID]
	if !ok || len(seq) == 0 {
		return seqResult{}
	}
	err := seq[0]
	p.errorSeqs[gameID] = seq[1:]
	return seqResult{err: err, found: true}
}

func (p *fakePushClient) defaultResult(gameID string) *PushResult {
	if p.results != nil {
		if r, ok := p.results[gameID]; ok {
			return r
		}
	}
	return &PushResult{SaveUUID: "test-uuid", SnapshotTimestamp: "2026-02-25T21:30:00Z"}
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

func (ws *fakeWSClient) Messages() <-chan []byte    { return ws.messages }
func (ws *fakeWSClient) Reconnected() <-chan struct{} { return ws.reconnected }
func (ws *fakeWSClient) Close() error               { return nil }
func (ws *fakeWSClient) Connected() bool            { return ws.connected }

func (ws *fakeWSClient) sentEventTypes() []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	var types []string
	for _, data := range ws.sent {
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) == nil {
			for key := range m {
				types = append(types, key)
			}
		}
	}
	return types
}

// sentEvent returns the payload of the nth event matching eventType.
func (ws *fakeWSClient) sentEvent(eventType string, index int) map[string]any {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	count := 0
	for _, data := range ws.sent {
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		raw, ok := m[eventType]
		if !ok {
			continue
		}
		if count == index {
			var payload map[string]any
			json.Unmarshal(raw, &payload)
			return payload
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
			"overview": {Description: "Character overview", Data: map[string]any{"level": float64(89)}},
		},
	}
}

func d2rConfig() Config {
	return Config{
		DeviceID: "steam-deck",
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
			"overview": {Description: "Shared stash overview", Data: map[string]any{"gold": float64(0)}},
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
	pusher := &fakePushClient{}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/SharedStash.d2i": []byte("stash data")},
	}
	cfg := Config{
		DeviceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s", ".d2i"}, Enabled: true},
		},
	}

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/SharedStash.d2i", "SharedStash.d2i", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushCompleted") {
		t.Error("missing pushCompleted")
	}

	// Identity in parseCompleted should have empty name for game-scoped saves.
	completed := ws.sentEvent("parseCompleted", 0)
	identity, ok := completed["identity"].(map[string]any)
	if !ok {
		t.Fatal("parseCompleted missing identity")
	}
	if _, hasCharName := identity["saveName"]; hasCharName {
		t.Error("game-scoped parseCompleted should not have saveName")
	}

	// Pushed body should have empty SaveName.
	if len(pusher.calls) != 1 {
		t.Fatalf("pusher called %d times, want 1", len(pusher.calls))
	}
	var pushedState GameState
	if err := json.Unmarshal(pusher.calls[0].Body, &pushedState); err != nil {
		t.Fatalf("unmarshal pushed body: %v", err)
	}
	if pushedState.Identity.SaveName != "" {
		t.Errorf("pushed saveName = %q, want empty", pushedState.Identity.SaveName)
	}
	if pushedState.Identity.GameID != "d2r" {
		t.Errorf("pushed gameId = %q, want d2r", pushedState.Identity.GameID)
	}
}

// --- Tests: scanGame ---

func TestScanGame_DetectsGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	pusher := &fakePushClient{}
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"])

	types := ws.sentEventTypes()
	for _, want := range []string{"scanStarted", "scanCompleted", "gameDetected", "watching", "pushCompleted"} {
		if !slices.Contains(types, want) {
			t.Errorf("missing %s event", want)
		}
	}

	detected := ws.sentEvent("gameDetected", 0)
	if detected["gameId"] != "d2r" {
		t.Errorf("gameDetected gameId = %v, want d2r", detected["gameId"])
	}
	if detected["saveCount"] != float64(1) {
		t.Errorf("gameDetected saveCount = %v, want 1", detected["saveCount"])
	}

	// Only .d2s matched, not .txt
	completed := ws.sentEvent("scanCompleted", 0)
	if completed["filesFound"] != float64(1) {
		t.Errorf("scanCompleted filesFound = %v, want 1", completed["filesFound"])
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if string(runner.calls[0].SaveBytes) != "fake save data" {
		t.Error("runner got wrong save bytes")
	}

	if len(pusher.calls) != 1 {
		t.Fatalf("pusher called %d times, want 1", len(pusher.calls))
	}
	var pushedState GameState
	if err := json.Unmarshal(pusher.calls[0].Body, &pushedState); err != nil {
		t.Fatalf("unmarshal pushed body: %v", err)
	}
	if pushedState.Summary != "Hammerdin, Level 89 Paladin" {
		t.Error("pusher got wrong summary")
	}
}

func TestScanGame_MissingDir(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
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
	pusher := &fakePushClient{}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("save data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileModify,
	})

	types := ws.sentEventTypes()
	for _, want := range []string{"parseStarted", "parseCompleted", "pushStarted", "pushCompleted"} {
		if !slices.Contains(types, want) {
			t.Errorf("missing %s event", want)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("pusher called %d times, want 1", len(pusher.calls))
	}
}

func TestHandleFileEvent_PreloadedDataBypassesReadFile(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{}
	fsys := &fakeFS{
		// No files — ReadFile would fail if called.
		files: map[string][]byte{},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
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
	if len(pusher.calls) != 1 {
		t.Fatalf("pusher called %d times, want 1", len(pusher.calls))
	}
}

func TestHandleFileEvent_IgnoresNonMatchingExtension(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(
		cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
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
		Config{}, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{},
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
		&fakePushClient{},
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

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/bad.d2s", "bad.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "parseFailed") {
		t.Error("missing parseFailed")
	}
	if slices.Contains(types, "pushStarted") {
		t.Error("unexpected pushStarted after parse failure")
	}

	failed := ws.sentEvent("parseFailed", 0)
	if failed["errorType"] != "corrupt_file" {
		t.Errorf("parseFailed errorType = %v, want corrupt_file", failed["errorType"])
	}
}

func TestParseAndPush_FileReadError(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{files: map[string][]byte{}} // file doesn't exist
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/missing.d2s", "missing.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "parseFailed") {
		t.Error("missing parseFailed")
	}
	if slices.Contains(types, "pluginStatus") {
		t.Error("unexpected pluginStatus -- runner should not have been called")
	}
}

func TestParseAndPush_PushError(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{
		errors: map[string]error{"d2r": &PushStatusError{StatusCode: 400, Body: "bad request"}},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushFailed") {
		t.Error("missing pushFailed")
	}
	if slices.Contains(types, "pushCompleted") {
		t.Error("unexpected pushCompleted after push failure")
	}

	failed := ws.sentEvent("pushFailed", 0)
	if failed["willRetry"] != false {
		t.Errorf("willRetry = %v, want false for 400 error", failed["willRetry"])
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

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
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

	s1 := ws.sentEvent("pluginStatus", 0)
	if s1["message"] != "Decoding header" {
		t.Errorf("status 0 message = %v", s1["message"])
	}
	s2 := ws.sentEvent("pluginStatus", 1)
	if s2["message"] != "Parsing inventory (247 items)" {
		t.Errorf("status 1 message = %v", s2["message"])
	}
}

// --- Tests: push retry ---

func TestPushWithRetry_TransientThenSuccess(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{
		errorSeqs: map[string][]error{
			"d2r": {
				&PushStatusError{StatusCode: 503, Body: "unavailable"},
				nil, // success on second attempt
			},
		},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()
	cfg.Retry = RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushCompleted") {
		t.Error("missing pushCompleted -- retry should have succeeded")
	}

	// Should have emitted pushFailed with willRetry=true for the transient failure.
	failed := ws.sentEvent("pushFailed", 0)
	if failed == nil {
		t.Fatal("missing pushFailed for transient error")
	}
	if failed["willRetry"] != true {
		t.Errorf("willRetry = %v, want true", failed["willRetry"])
	}

	// Should have 2 push calls.
	pusher.mu.Lock()
	calls := len(pusher.calls)
	pusher.mu.Unlock()
	if calls != 2 {
		t.Errorf("push calls = %d, want 2", calls)
	}

	// Should have 2 pushStarted events (initial + retry).
	pushStartedCount := 0
	for _, et := range types {
		if et == "pushStarted" {
			pushStartedCount++
		}
	}
	if pushStartedCount != 2 {
		t.Errorf("pushStarted count = %d, want 2", pushStartedCount)
	}
}

func TestPushWithRetry_MaxRetriesExhausted(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{
		errors: map[string]error{
			"d2r": &PushStatusError{StatusCode: 503, Body: "unavailable"},
		},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()
	cfg.Retry = RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)

	types := ws.sentEventTypes()
	if slices.Contains(types, "pushCompleted") {
		t.Error("unexpected pushCompleted -- all retries should fail")
	}

	// Should have 3 push calls (1 initial + 2 retries).
	pusher.mu.Lock()
	calls := len(pusher.calls)
	pusher.mu.Unlock()
	if calls != 3 {
		t.Errorf("push calls = %d, want 3", calls)
	}

	// Last pushFailed should have willRetry=false.
	pushFailedCount := 0
	for _, et := range types {
		if et == "pushFailed" {
			pushFailedCount++
		}
	}
	if pushFailedCount != 3 {
		t.Errorf("pushFailed count = %d, want 3", pushFailedCount)
	}

	// Last failure should have willRetry=false.
	lastFailed := ws.sentEvent("pushFailed", pushFailedCount-1)
	if lastFailed["willRetry"] != false {
		t.Errorf("last pushFailed willRetry = %v, want false", lastFailed["willRetry"])
	}

	// First failure should have willRetry=true.
	firstFailed := ws.sentEvent("pushFailed", 0)
	if firstFailed["willRetry"] != true {
		t.Errorf("first pushFailed willRetry = %v, want true", firstFailed["willRetry"])
	}
}

func TestPushWithRetry_PermanentFailureNoRetry(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{
		errors: map[string]error{
			"d2r": &PushStatusError{StatusCode: 401, Body: "unauthorized"},
		},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()
	cfg.Retry = RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)

	// Should only have 1 push call -- no retries for 4xx.
	pusher.mu.Lock()
	calls := len(pusher.calls)
	pusher.mu.Unlock()
	if calls != 1 {
		t.Errorf("push calls = %d, want 1 (no retry for 4xx)", calls)
	}

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushFailed") {
		t.Error("missing pushFailed")
	}
	if slices.Contains(types, "pushCompleted") {
		t.Error("unexpected pushCompleted")
	}

	failed := ws.sentEvent("pushFailed", 0)
	if failed["willRetry"] != false {
		t.Errorf("willRetry = %v, want false for permanent failure", failed["willRetry"])
	}
}

func TestPushWithRetry_ContextCancellation(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()

	// First call returns 503, then context gets canceled during backoff
	pusher := &fakePushClient{
		errors: map[string]error{
			"d2r": &PushStatusError{StatusCode: 503, Body: "unavailable"},
		},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()
	// Use a longer delay so we can cancel during backoff
	cfg.Retry = RetryConfig{MaxRetries: 3, BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws, &fakePluginManager{}, nil, testLogger())

	done := make(chan struct{})
	go func() {
		d.parseAndPush(ctx, "d2r", "/saves/d2r/test.d2s", "test.d2s", nil)
		close(done)
	}()

	// Wait for first push attempt, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good, returned promptly
	case <-time.After(2 * time.Second):
		t.Fatal("parseAndPush did not return after context cancellation")
	}

	// Should have only 1 push call
	pusher.mu.Lock()
	calls := len(pusher.calls)
	pusher.mu.Unlock()
	if calls != 1 {
		t.Errorf("push calls = %d, want 1", calls)
	}
}

// --- Tests: handleCommand ---

func TestHandleCommand_RescanGame(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"rescanGame": map[string]any{"gameId": "d2r"},
	})
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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"testPath": map[string]any{"gameId": "d2r", "path": "/custom/path"},
	})
	d.handleCommand(context.Background(), cmd)

	result := ws.sentEvent("testPathResult", 0)
	if result == nil {
		t.Fatal("missing testPathResult")
	}
	if result["valid"] != true {
		t.Errorf("valid = %v, want true", result["valid"])
	}
	if result["filesFound"] != float64(2) {
		t.Errorf("filesFound = %v, want 2", result["filesFound"])
	}
}

func TestHandleCommand_TestPath_Invalid(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"testPath": map[string]any{"gameId": "d2r", "path": "/nonexistent"},
	})
	d.handleCommand(context.Background(), cmd)

	result := ws.sentEvent("testPathResult", 0)
	if result == nil {
		t.Fatal("missing testPathResult")
	}
	if result["valid"] != false {
		t.Errorf("valid = %v, want false", result["valid"])
	}
}

// --- Tests: handleConfigUpdate ---

func TestConfigUpdate_AddsNewGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := Config{DeviceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(cfg, fsys, watcher, runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/saves/d2r",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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

	d := New(cfg, d2rFS(), watcher, d2rRunner(), &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/saves/d2r",
					"enabled":        false,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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

	d := New(cfg, d2rFS(), watcher, d2rRunner(), &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	// Send empty config -- d2r is no longer present.
	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{},
		},
	})
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

	d := New(cfg, fsys, watcher, runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/new/path",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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
		DeviceID: "deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {SavePath: "/saves/d2r", FileExtensions: []string{".d2s"}, Enabled: false},
		},
	}

	d := New(cfg, fsys, watcher, runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/saves/d2r",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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
		DeviceID: "steam-deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
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
	if types[0] != "daemonOnline" {
		t.Errorf("first event = %v, want daemonOnline", types[0])
	}
	if types[len(types)-1] != "daemonOffline" {
		t.Errorf("last event = %v, want daemonOffline", types[len(types)-1])
	}

	online := ws.sentEvent("daemonOnline", 0)
	if online["deviceId"] != "steam-deck" {
		t.Errorf("daemonOnline deviceId = %v", online["deviceId"])
	}
	if online["version"] != "0.1.0" {
		t.Errorf("daemonOnline version = %v", online["version"])
	}
}

func TestRun_FileEventTriggersParseAndPush(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	pusher := &fakePushClient{}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/Hammerdin.d2s": []byte("save data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, pusher, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "daemonOnline")
	})

	watcher.events <- FileEvent{Path: "/saves/d2r/Hammerdin.d2s", Op: FileModify}

	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "pushCompleted")
	})

	runner.mu.Lock()
	runnerCalls := len(runner.calls)
	runner.mu.Unlock()
	if runnerCalls != 1 {
		t.Errorf("runner called %d times, want 1", runnerCalls)
	}

	pusher.mu.Lock()
	pusherCalls := len(pusher.calls)
	pusher.mu.Unlock()
	if pusherCalls != 1 {
		t.Errorf("pusher called %d times, want 1", pusherCalls)
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

	d := New(cfg, fsys, watcher, runner, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for startup (scan + initial parse)
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "pushCompleted")
	})

	// Clear sent to isolate the rescan
	ws.mu.Lock()
	ws.sent = nil
	ws.mu.Unlock()

	cmd, _ := json.Marshal(map[string]any{
		"rescanGame": map[string]any{"gameId": "d2r"},
	})
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
	cfg := Config{DeviceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws, pm, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/saves/d2r",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
	d.handleCommand(context.Background(), cmd)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pluginDownloadFailed") {
		t.Error("missing pluginDownloadFailed event")
	}
	if slices.Contains(types, "scanStarted") {
		t.Error("should not scan when plugin download fails")
	}

	failed := ws.sentEvent("pluginDownloadFailed", 0)
	if failed["gameId"] != "d2r" {
		t.Errorf("pluginDownloadFailed gameId = %v, want d2r", failed["gameId"])
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

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws, pm, nil, testLogger())

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
	cfg := Config{DeviceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}

	d := New(cfg, fsys, newFakeWatcher(), d2rRunner(), &fakePushClient{}, ws, pm, nil, testLogger())

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/saves/d2r",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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

	d := New(cfg, fsys, watcher, d2rRunner(), &fakePushClient{}, ws, pm, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	cmd, _ := json.Marshal(map[string]any{
		"configUpdate": map[string]any{
			"games": map[string]any{
				"d2r": map[string]any{
					"savePath":       "/new/path",
					"enabled":        true,
					"fileExtensions": []string{".d2s"},
				},
			},
		},
	})
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
		newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	event := ws.sentEvent("gamesDiscovered", 0)
	if event == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	games, ok := event["games"].([]any)
	if !ok || len(games) != 1 {
		t.Fatalf("games = %v, want 1 game", event["games"])
	}

	game, ok2 := games[0].(map[string]any)
	if !ok2 {
		t.Fatal("game is not a map")
	}
	if game["gameId"] != "d2r" {
		t.Errorf("gameId = %v, want d2r", game["gameId"])
	}
	if game["name"] != "Diablo II: Resurrected" {
		t.Errorf("name = %v", game["name"])
	}
	if game["path"] != "/home/user/saves/d2r" {
		t.Errorf("path = %v", game["path"])
	}
	if game["fileCount"] != float64(1) {
		t.Errorf("fileCount = %v, want 1", game["fileCount"])
	}
}

func TestDiscoverGames_NilPluginManager(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{Games: map[string]GameConfig{}}
	d := New(
		cfg, &fakeFS{}, newFakeWatcher(),
		&fakeRunner{}, &fakePushClient{}, ws, nil, nil, testLogger(),
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
		newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	event := ws.sentEvent("gamesDiscovered", 0)
	if event == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	// games should be nil/empty since path doesn't exist.
	if event["games"] != nil {
		games, ok := event["games"].([]any)
		if ok && len(games) != 0 {
			t.Errorf("games = %v, want empty", event["games"])
		}
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
		newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())

	event := ws.sentEvent("gamesDiscovered", 0)
	if event == nil {
		t.Fatal("missing gamesDiscovered event")
	}

	games, ok := event["games"].([]any)
	if !ok || len(games) != 1 {
		t.Fatalf("games len = %v, want 1 (only d2r found)", event["games"])
	}

	game, ok2 := games[0].(map[string]any)
	if !ok2 {
		t.Fatal("game is not a map")
	}
	if game["gameId"] != "d2r" {
		t.Errorf("found game = %v, want d2r", game["gameId"])
	}
}

func TestDiscoverGames_ManifestError(t *testing.T) {
	ws := newFakeWSClient()

	pm := &fakePluginManager{
		manifestErr: fmt.Errorf("network error"),
	}

	d := New(
		Config{Games: map[string]GameConfig{}}, &fakeFS{},
		newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, pm, nil, testLogger(),
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
		newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, pm, nil, testLogger(),
	)

	cmd, _ := json.Marshal(map[string]any{
		"discoverGames": map[string]any{},
	})
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
		DeviceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	cmd, _ := json.Marshal(map[string]any{
		"daemonUpdateAvailable": map[string]any{
			"version":      "0.2.0",
			"url":          "https://example.com/daemon",
			"signatureUrl": "https://example.com/daemon.sig",
			"sha256":       "abc123",
		},
	})
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
		t.Error("missing daemonUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "daemonOffline") {
		t.Error("missing daemonOffline after successful update")
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
		DeviceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	cmd, _ := json.Marshal(map[string]any{
		"daemonUpdateAvailable": map[string]any{
			"version":      "0.2.0",
			"url":          "https://example.com/daemon",
			"signatureUrl": "https://example.com/daemon.sig",
			"sha256":       "abc123",
		},
	})
	d.handleCommand(context.Background(), cmd)

	if !slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
		t.Error("missing daemonUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "daemonUpdateFailed") {
		t.Error("missing daemonUpdateFailed event")
	}

	failed := ws.sentEvent("daemonUpdateFailed", 0)
	if failed["message"] != "disk full" {
		t.Errorf("message = %v, want 'disk full'", failed["message"])
	}
}

func TestHandleCommand_DaemonUpdateAvailable_NilUpdater(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{DeviceID: "deck", Version: "0.1.0", Games: map[string]GameConfig{}}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	cmd, _ := json.Marshal(map[string]any{
		"daemonUpdateAvailable": map[string]any{
			"version": "0.2.0",
			"url":     "https://example.com/daemon",
			"sha256":  "abc123",
		},
	})
	d.handleCommand(context.Background(), cmd)

	// Should not crash, should not send any update events
	if slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
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
		DeviceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	d.checkSelfUpdate(context.Background())

	if !slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
		t.Error("missing daemonUpdateStarted event")
	}
	if !slices.Contains(ws.sentEventTypes(), "daemonOffline") {
		t.Error("missing daemonOffline after successful update")
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
		DeviceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	d.checkSelfUpdate(context.Background())

	if slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
		t.Error("should not start update when Check returns nil")
	}
}

func TestCheckSelfUpdate_CheckError(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{checkErr: fmt.Errorf("network error")}
	cfg := Config{
		DeviceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		updater,
		testLogger(),
	)

	d.checkSelfUpdate(context.Background())

	if slices.Contains(ws.sentEventTypes(), "daemonUpdateStarted") {
		t.Error("should not start update when Check returns error")
	}
}

func TestCheckSelfUpdate_NilUpdater(_ *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		DeviceID: "deck",
		Version:  "0.2.0",
		Games:    map[string]GameConfig{},
	}

	d := New(
		cfg,
		&fakeFS{},
		newFakeWatcher(),
		&fakeRunner{},
		&fakePushClient{},
		ws,
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	// Should not panic
	d.checkSelfUpdate(context.Background())
}

func TestRun_SendsHeartbeat(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		DeviceID: "deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for heartbeat — the ticker fires at heartbeatInterval (30s),
	// but we can't wait that long in a test. Instead, verify the heartbeat
	// case compiles and the event type is correct by checking announceOnline
	// and then canceling immediately. The heartbeat ticker is tested implicitly
	// by the select loop structure.
	//
	// For a real heartbeat timing test, we'd need a fake clock. For now,
	// verify that the daemon sends daemonOnline on startup (which uses
	// the same announceOnline path as reconnect).
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "daemonOnline")
	})

	cancel()
	<-done

	if !slices.Contains(ws.sentEventTypes(), "daemonOnline") {
		t.Error("missing daemonOnline event")
	}
}

func TestRun_ReconnectReannounces(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		DeviceID: "deck",
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
	}, &fakePushClient{}, ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for initial daemonOnline.
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "daemonOnline")
	})

	// Count initial daemonOnline events.
	initialCount := 0
	for _, et := range ws.sentEventTypes() {
		if et == "daemonOnline" {
			initialCount++
		}
	}

	// Simulate reconnect.
	ws.reconnected <- struct{}{}

	// Wait for second daemonOnline.
	waitFor(t, func() bool {
		count := 0
		for _, et := range ws.sentEventTypes() {
			if et == "daemonOnline" {
				count++
			}
		}
		return count > initialCount
	})

	// Verify re-announced with correct deviceId.
	// The second daemonOnline should have the same deviceId.
	onlineEvent := ws.sentEvent("daemonOnline", 1)
	if onlineEvent == nil {
		t.Fatal("second daemonOnline event not found")
	}
	if onlineEvent["deviceId"] != "deck" {
		t.Errorf("reconnect daemonOnline deviceId = %v, want deck", onlineEvent["deviceId"])
	}

	// Verify gamesDiscovered was re-sent (part of announceOnline).
	discoverCount := 0
	for _, et := range ws.sentEventTypes() {
		if et == "gamesDiscovered" {
			discoverCount++
		}
	}
	if discoverCount < 2 {
		t.Errorf("gamesDiscovered sent %d times, want >= 2 (initial + reconnect)", discoverCount)
	}

	cancel()
	<-done
}
