package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// --- Fakes ---

type fakeFS struct {
	files map[string][]byte   // full path → contents
	dirs  map[string][]string // dir path → file names
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
	events chan FileEvent
	added  []string
	mu     sync.Mutex
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

func (w *fakeWatcher) Events() <-chan FileEvent { return w.events }
func (w *fakeWatcher) Close() error             { return nil }

type fakeRunner struct {
	results    map[string]*GameState
	errors     map[string]error
	statusMsgs map[string][]string // gameID → status messages to emit
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
	results map[string]*PushResult
	errors  map[string]error
	calls   []pushCall
	mu      sync.Mutex
}

type pushCall struct {
	GameID string
	State  *GameState
}

func (p *fakePushClient) Push(
	_ context.Context,
	gameID string,
	state *GameState,
	_ time.Time,
) (*PushResult, error) {
	p.mu.Lock()
	p.calls = append(p.calls, pushCall{GameID: gameID, State: state})
	p.mu.Unlock()

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

type fakeWSClient struct {
	messages  chan []byte
	sent      [][]byte
	connected bool
	mu        sync.Mutex
}

func newFakeWSClient() *fakeWSClient {
	return &fakeWSClient{messages: make(chan []byte, 10)}
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

func (ws *fakeWSClient) Messages() <-chan []byte { return ws.messages }
func (ws *fakeWSClient) Close() error            { return nil }

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
			CharacterName: "Hammerdin",
			GameID:        "d2r",
			Extra:         map[string]any{"class": "Paladin", "level": float64(89)},
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

// --- Tests: scanGame ---

func TestScanGame_DetectsGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := d2rRunner()
	pusher := &fakePushClient{}
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, watcher, runner, pusher, ws)
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
	if pusher.calls[0].State.Summary != "Hammerdin, Level 89 Paladin" {
		t.Error("pusher got wrong summary")
	}
}

func TestScanGame_MissingDir(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{}, files: map[string][]byte{}}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
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

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws)
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

func TestHandleFileEvent_IgnoresNonMatchingExtension(t *testing.T) {
	ws := newFakeWSClient()
	cfg := d2rConfig()

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
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

	d := New(Config{}, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
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

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
	// watchedDirs is empty — no directories are being watched

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

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws)
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/bad.d2s", "bad.d2s")

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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/missing.d2s", "missing.d2s")

	types := ws.sentEventTypes()
	if !slices.Contains(types, "parseFailed") {
		t.Error("missing parseFailed")
	}
	if slices.Contains(types, "pluginStatus") {
		t.Error("unexpected pluginStatus — runner should not have been called")
	}
}

func TestParseAndPush_PushError(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	pusher := &fakePushClient{
		errors: map[string]error{"d2r": fmt.Errorf("server unavailable")},
	}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/d2r/test.d2s": []byte("data")},
	}
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, pusher, ws)
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s")

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushFailed") {
		t.Error("missing pushFailed")
	}
	if slices.Contains(types, "pushCompleted") {
		t.Error("unexpected pushCompleted after push failure")
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

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws)
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s")

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

// --- Tests: handleCommand ---

func TestHandleCommand_RescanGame(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, &fakePushClient{}, ws)

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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)

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

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)

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

// --- Tests: Run lifecycle ---

func TestRun_LifecycleEvents(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		DeviceID: "steam-deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, &fakePushClient{}, ws)

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

	d := New(cfg, fsys, watcher, runner, pusher, ws)
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

	d := New(cfg, fsys, watcher, runner, &fakePushClient{}, ws)

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
