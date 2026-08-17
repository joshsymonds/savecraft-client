package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/joshsymonds/savecraft-client/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft-client/internal/proto/savecraft/v1"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// recordingHandler is a minimal slog.Handler that captures emitted records,
// used to assert a specific warning/error was logged (e.g. the S3/S4
// directory-unit size-cap and read-failure skips) without depending on any
// particular log output formatting.
type recordingHandler struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

// newRecordingLogger returns a logger backed by recordingHandler and an
// accessor for a snapshot of the records captured so far.
func newRecordingLogger() (*slog.Logger, func() []slog.Record) {
	var records []slog.Record
	h := &recordingHandler{mu: &sync.Mutex{}, records: &records}
	return slog.New(h), func() []slog.Record {
		h.mu.Lock()
		defer h.mu.Unlock()
		return append([]slog.Record(nil), *h.records...)
	}
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// --- Fakes ---

// testAllSaveRoots makes the save-path allowlist permissive (filesystem
// root). Used by config/test-path mechanics tests that exercise unrelated
// behavior with synthetic fixture paths; the allowlist itself is tested
// separately in TestSaveRootAllowed and friends.
func testAllSaveRoots() []string { return []string{string(filepath.Separator)} }

type fakeFS struct {
	files         map[string][]byte   // full path -> contents
	dirs          map[string][]string // dir path -> file names
	symlinks      map[string]string   // path -> resolved target (symlink escape tests)
	readFileCount int                 // number of ReadFile calls (for verifying bypass)
	readDirCalls  []string            // paths passed to ReadDir, in call order (for verifying recursion never attempted)
}

// EvalSymlinks mimics filepath.EvalSymlinks: returns the mapped target for a
// symlink, the path itself when it exists, or os.ErrNotExist otherwise.
func (f *fakeFS) EvalSymlinks(path string) (string, error) {
	if target, ok := f.symlinks[path]; ok {
		return target, nil
	}
	if _, ok := f.dirs[path]; ok {
		return path, nil
	}
	if _, ok := f.files[path]; ok {
		return path, nil
	}
	return "", os.ErrNotExist
}

// Stat mimics os.Stat's symlink-following: a path registered in f.symlinks
// resolves through to its target before being classified, so a symlink to a
// directory reports IsDir() true, exactly like the real filesystem.
func (f *fakeFS) Stat(path string) (fs.FileInfo, error) {
	if target, ok := f.symlinks[path]; ok {
		return f.Stat(target)
	}
	if _, ok := f.dirs[path]; ok {
		return &fakeFileInfo{name: filepath.Base(path), dir: true}, nil
	}
	if data, ok := f.files[path]; ok {
		return &fakeFileInfo{name: filepath.Base(path), size: int64(len(data))}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeFS) ReadDir(path string) ([]fs.DirEntry, error) {
	f.readDirCalls = append(f.readDirCalls, path)
	names, ok := f.dirs[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	entries := make([]fs.DirEntry, len(names))
	for i, name := range names {
		// IsDir/Type reflect only whether the child is itself a registered
		// directory (f.dirs) or symlink (f.symlinks), never resolving a
		// symlink target — mirroring a real os.DirEntry, which reports a
		// symlink's own (non-dir) type without following it.
		full := filepath.Join(path, name)
		_, isDir := f.dirs[full]
		_, isSymlink := f.symlinks[full]
		entries[i] = &fakeDirEntry{name: name, dir: isDir, symlink: isSymlink}
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

type fakeDirEntry struct {
	name    string
	dir     bool
	symlink bool
}

func (de *fakeDirEntry) Name() string {
	return de.name
}

func (de *fakeDirEntry) IsDir() bool {
	return de.dir
}

func (de *fakeDirEntry) Type() fs.FileMode {
	if de.symlink {
		return fs.ModeSymlink
	}
	if de.dir {
		return fs.ModeDir
	}
	return 0
}

func (de *fakeDirEntry) Info() (fs.FileInfo, error) {
	return &fakeFileInfo{name: de.name, dir: de.dir}, nil
}

type fakeWatcher struct {
	events        chan FileEvent
	added         []string
	addedDirUnits []addDirUnitCall
	removed       []string
	mu            sync.Mutex
}

type addDirUnitCall struct {
	Root        string
	ExcludeDirs []string
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

func (w *fakeWatcher) AddDirectoryUnit(root string, excludeDirs []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.addedDirUnits = append(w.addedDirUnits, addDirUnitCall{Root: root, ExcludeDirs: excludeDirs})
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

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
	state   *GameState
}

func (r *blockingRunner) Run(_ context.Context, _ string, _ string, _ []byte, _ func(string)) (*GameState, error) {
	close(r.started)
	<-r.release
	return r.state, nil
}

type runCall struct {
	GameID    string
	FileName  string
	SaveBytes []byte
}

func (r *fakeRunner) Run(
	_ context.Context,
	gameID string,
	fileName string,
	saveBytes []byte,
	onStatus func(string),
) (*GameState, error) {
	r.mu.Lock()
	r.calls = append(r.calls, runCall{GameID: gameID, FileName: fileName, SaveBytes: saveBytes})
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
	messages      chan []byte
	connected     chan struct{}
	sent          [][]byte
	isConnected   bool
	manualConnect bool  // if true, Start does not auto-signal the first connection
	sendErr       error // if non-nil, Send returns this error
	failNextSend  error // if non-nil, one Send returns this error
	mu            sync.Mutex
}

func newFakeWSClient() *fakeWSClient {
	return &fakeWSClient{
		messages:  make(chan []byte, 10),
		connected: make(chan struct{}, 1),
	}
}

// Start mirrors the real client: it marks the connection live and signals the
// first connection so the daemon announces online. With manualConnect set, it
// stays disconnected so a test can drive the first connection explicitly
// (simulating a connection that isn't available yet at startup).
func (ws *fakeWSClient) Start(_ context.Context) {
	if ws.manualConnect {
		return
	}
	ws.isConnected = true
	ws.signalConnected()
}

// signalConnected does the drain-then-send used by the real client, so a
// signal is never lost but never blocks.
func (ws *fakeWSClient) signalConnected() {
	select {
	case <-ws.connected:
	default:
	}
	ws.connected <- struct{}{}
}

func (ws *fakeWSClient) Send(msg []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.failNextSend != nil {
		err := ws.failNextSend
		ws.failNextSend = nil
		return err
	}
	if ws.sendErr != nil {
		return ws.sendErr
	}
	cp := make([]byte, len(msg))
	copy(cp, msg)
	ws.sent = append(ws.sent, cp)
	return nil
}

func TestParseAndPush_ParseFailureDeduplication(t *testing.T) {
	const path = "/saves/d2r/bad.d2s"
	newDaemon := func(ws *fakeWSClient, runner *fakeRunner) *Daemon {
		return New(d2rConfig(), &fakeFS{}, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	}
	runFailure := func(d *Daemon, data []byte) {
		d.parseAndPush(context.Background(), "d2r", path, "bad.d2s", data, false)
	}

	t.Run("identical repetition", func(t *testing.T) {
		ws := newFakeWSClient()
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("bad")}}
		d := newDaemon(ws, r)
		runFailure(d, []byte("same"))
		runFailure(d, []byte("same"))
		if got := countEventType(ws, "parseFailed"); got != 1 {
			t.Fatalf("parseFailed count = %d, want 1", got)
		}
	})

	t.Run("content change", func(t *testing.T) {
		ws := newFakeWSClient()
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("bad")}}
		d := newDaemon(ws, r)
		runFailure(d, []byte("one"))
		runFailure(d, []byte("two"))
		if got := countEventType(ws, "parseFailed"); got != 2 {
			t.Fatalf("parseFailed count = %d, want 2", got)
		}
	})

	t.Run("error change", func(t *testing.T) {
		ws := newFakeWSClient()
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("first")}}
		d := newDaemon(ws, r)
		runFailure(d, []byte("same"))
		r.errors["d2r"] = errors.New("second")
		runFailure(d, []byte("same"))
		if got := countEventType(ws, "parseFailed"); got != 2 {
			t.Fatalf("parseFailed count = %d, want 2", got)
		}
	})

	t.Run("failure success failure", func(t *testing.T) {
		ws := newFakeWSClient()
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("bad")}, results: map[string]*GameState{"d2r": {}}}
		d := newDaemon(ws, r)
		runFailure(d, []byte("same"))
		delete(r.errors, "d2r")
		runFailure(d, []byte("same"))
		r.errors["d2r"] = errors.New("bad")
		runFailure(d, []byte("same"))
		if got := countEventType(ws, "parseFailed"); got != 2 {
			t.Fatalf("parseFailed count = %d, want 2", got)
		}
	})

	t.Run("restart", func(t *testing.T) {
		ws := newFakeWSClient()
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("bad")}}
		d := newDaemon(ws, r)
		runFailure(d, []byte("same"))
		runFailure(newDaemon(ws, r), []byte("same"))
		if got := countEventType(ws, "parseFailed"); got != 2 {
			t.Fatalf("parseFailed count = %d, want 2", got)
		}
	})

	t.Run("failed delivery retries", func(t *testing.T) {
		ws := newFakeWSClient()
		ws.failNextSend = errors.New("offline")
		r := &fakeRunner{errors: map[string]error{"d2r": errors.New("bad")}}
		d := newDaemon(ws, r)
		d.parseAndPush(context.Background(), "d2r", path, "bad.d2s", []byte("same"), true)
		if _, ok := d.parseFailures[path]; ok {
			t.Fatal("failed ParseFailed delivery recorded suppression")
		}
		d.parseAndPush(context.Background(), "d2r", path, "bad.d2s", []byte("same"), true)
		if got := countEventType(ws, "parseFailed"); got != 1 {
			t.Fatalf("parseFailed count = %d, want 1 after retry", got)
		}
	})
}

func TestParseAndPush_DropsStalePluginGeneration(t *testing.T) {
	ws := newFakeWSClient()
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{}), state: &GameState{
		Identity: Identity{GameID: "d2r", SaveName: "Hero"},
		Sections: map[string]Section{"header": {Data: jsontext.Value(`{"level":1}`)}},
	}}
	d := New(d2rConfig(), &fakeFS{files: map[string][]byte{
		"/saves/Hero.d2s": []byte("save"),
	}}, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs = map[string]string{}
	done := make(chan struct{})
	go func() {
		d.parseAndPush(context.Background(), "d2r", "/saves/Hero.d2s", "Hero.d2s", nil, true)
		close(done)
	}()
	<-runner.started
	d.pluginChanged(context.Background(), "d2r", d.cfg.Games["d2r"])
	close(runner.release)
	<-done
	if got := countEventType(ws, "pushSave"); got != 0 {
		t.Fatalf("stale pushSave count = %d, want 0", got)
	}
	if _, ok := d.lastPushedSectionHashes["/saves/Hero.d2s"]; ok {
		t.Fatal("stale parse repopulated section cache")
	}
	if _, ok := d.parseFailures["/saves/Hero.d2s"]; ok {
		t.Fatal("stale parse repopulated failure record")
	}
}

func (ws *fakeWSClient) Messages() <-chan []byte    { return ws.messages }
func (ws *fakeWSClient) Connected() <-chan struct{} { return ws.connected }
func (ws *fakeWSClient) ForceReconnect()            {}
func (ws *fakeWSClient) Close() error               { return nil }
func (ws *fakeWSClient) IsConnected() bool          { return ws.isConnected }

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

// decodeProto gunzips (if needed) and unmarshals a proto Message from sent bytes.
func decodeProto(data []byte) (*pb.Message, error) {
	raw := data
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		raw, err = io.ReadAll(r)
		if err != nil {
			return nil, err
		}
	}
	var msg pb.Message
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (ws *fakeWSClient) sentEventTypes() []string {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	var types []string
	for _, data := range ws.sent {
		if msg, err := decodeProto(data); err == nil {
			types = append(types, protoTypeName(msg))
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
		msg, err := decodeProto(data)
		if err != nil {
			continue
		}
		if protoTypeName(msg) != eventType {
			continue
		}
		if count == index {
			if cloned, ok := proto.Clone(msg).(*pb.Message); ok {
				return cloned
			}
			return nil
		}
		count++
	}
	return nil
}

type fakePluginManager struct {
	ensured      []string
	ensureErr    map[string]error
	manifests    map[string]pluginmgr.PluginInfo
	manifestErr  error
	updateResult []string
	updateErr    error
	mu           sync.Mutex
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
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.updateResult, pm.updateErr
}

func (pm *fakePluginManager) Manifests(_ context.Context) (map[string]pluginmgr.PluginInfo, error) {
	if pm.manifestErr != nil {
		return nil, pm.manifestErr
	}
	return pm.manifests, nil
}

type fakeUpdater struct {
	checkResult *CheckResult
	checkErr    error
	applyErr    error
	applyCalls  []applyCall
	mu          sync.Mutex
}

type applyCall struct {
	Info       *UpdateInfo
	BinaryPath string
}

func (u *fakeUpdater) Check(_ context.Context, _, _ string) (*CheckResult, error) {
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

func TestFilterSaveFiles_RegexStardewLayout(t *testing.T) {
	entries := []fs.DirEntry{
		&fakeDirEntry{name: "Farmer_123456789"},
		&fakeDirEntry{name: "Farmer_123456789_old"},
		&fakeDirEntry{name: "SaveGameInfo"},
		&fakeDirEntry{name: "steam_autocloud.vdf"},
	}
	d := &Daemon{}
	got := d.filterSaveFiles(entries, nil, []string{"regex:^.+_[0-9]+$"}, nil)
	if !slices.Equal(got, []string{"Farmer_123456789"}) {
		t.Errorf("filterSaveFiles = %v, want [Farmer_123456789]", got)
	}
}

func TestHandleFileEvent_RegexPatternGuard(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	d := New(Config{Games: map[string]GameConfig{
		"d2r": {FilePatterns: []string{"regex:^.+_[0-9]+$"}, Enabled: true},
	}}, &fakeFS{}, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"
	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Farmer_123456789_old",
		Op:   FileModify,
		Data: []byte("old"),
	})
	if len(runner.calls) != 0 {
		t.Fatal("pattern guard allowed non-canonical save")
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

// newStashState returns a fixture matching the real d2r plugin's shape for a
// game-scoped save (plugins/d2r/parser/main.go:64): the .d2i shared stash
// has no per-player identity, but it is represented by a non-empty
// conventional name derived from the stash kind, not by an empty saveName.
func newStashState() *GameState {
	return &GameState{
		Identity: Identity{
			SaveName: "Shared Stash (Softcore)",
			GameID:   "d2r",
		},
		Summary: "Shared Stash (Softcore), 60 items, 0 gold",
		Sections: map[string]Section{
			"overview": {Description: "Shared stash overview", Data: jsontext.Value(`{"gold":0}`)},
		},
	}
}

// --- Tests: game-scoped identity ---

// TestGameScopedIdentity_NonEmptyNamePassesThrough guards the plugin
// contract: a game-scoped save (no per-player identity) is represented by a
// non-empty conventional name, never an empty saveName. An earlier version
// of this test asserted the opposite — that an empty saveName should
// marshal through unmodified for "game-scoped" saves — but that shape was
// stale: the wire contract requires a non-empty saveName and gameId, and no
// real plugin emits that shape — the real d2r plugin always
// derives a name from the stash kind (plugins/d2r/parser/main.go:64). Empty
// saveName now means "identity unknown" and is substituted by the daemon;
// see TestParseAndPush_EmptySaveNameFallsBackToUnknownPlayer.
func TestGameScopedIdentity_NonEmptyNamePassesThrough(t *testing.T) {
	state := newStashState()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(data, &raw); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}
	var identity map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(raw["identity"], &identity); unmarshalErr != nil {
		t.Fatalf("unmarshal identity: %v", unmarshalErr)
	}
	if string(identity["saveName"]) != `"Shared Stash (Softcore)"` {
		t.Errorf("saveName = %s, want %q", identity["saveName"], "Shared Stash (Softcore)")
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
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/SharedStash.d2i", "SharedStash.d2i", nil, false)

	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushSave") {
		t.Error("missing pushSave")
	}

	// The game-scoped name is non-empty, so it passes through unmodified —
	// the daemon's identity-unknown substitution must not touch it.
	msg := ws.sentProto("parseCompleted", 0)
	if msg == nil {
		t.Fatal("missing parseCompleted")
	}
	pc := msg.GetParseCompleted()
	if pc.Identity == nil {
		t.Fatal("parseCompleted missing identity")
	}
	if pc.Identity.Name != "Shared Stash (Softcore)" {
		t.Errorf("parseCompleted saveName = %q, want %q", pc.Identity.Name, "Shared Stash (Softcore)")
	}

	pushMsg := ws.sentProto("pushSave", 0)
	if pushMsg == nil {
		t.Fatal("missing pushSave")
	}
	ps := pushMsg.GetPushSave()
	if ps.Identity.Name != "Shared Stash (Softcore)" {
		t.Errorf("pushed saveName = %q, want %q", ps.Identity.Name, "Shared Stash (Softcore)")
	}
	if ps.GameId != "d2r" {
		t.Errorf("pushed gameId = %q, want d2r", ps.GameId)
	}
}

func TestToProtoIdentity_DisplayName(t *testing.T) {
	id := Identity{
		SaveName:    "Player1234",
		GameID:      "magic",
		DisplayName: "Player One",
	}

	si := toProtoIdentity(id)

	if si.Name != "Player1234" {
		t.Errorf("Name = %q, want Player1234", si.Name)
	}
	if si.DisplayName != "Player One" {
		t.Errorf("DisplayName = %q, want %q", si.DisplayName, "Player One")
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
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"], false)

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
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"], false)

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
	d.scanGame(context.Background(), "d2r", cfg.Games["d2r"], false)

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
	d.parseFailures["/saves/d2r/Hammerdin.d2s"] = parseFailure{gameID: "d2r"}
	d.handleFileEvent(context.Background(), FileEvent{
		Path: "/saves/d2r/Hammerdin.d2s",
		Op:   FileRemove,
	})

	if len(ws.sentEventTypes()) != 0 {
		t.Error("should not send events for file removal")
	}
	if _, ok := d.parseFailures["/saves/d2r/Hammerdin.d2s"]; ok {
		t.Fatal("file removal retained parse failure")
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

func TestToParseErrorType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  pb.ParseErrorType
	}{
		{
			name:  "unsupported version",
			input: "unsupported_version",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_UNSUPPORTED_VERSION,
		},
		{
			name:  "corrupt file",
			input: "corrupt_file",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_CORRUPT_FILE,
		},
		{
			name:  "parse error",
			input: "parse_error",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR,
		},
		{
			name:  "resource limit",
			input: "resource_limit",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_RESOURCE_LIMIT,
		},
		{
			name:  "read error",
			input: "read_error",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR,
		},
		{
			name:  "unknown",
			input: "anything_else",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_PARSE_ERROR,
		},
		{
			name:  "enum name",
			input: "PARSE_ERROR_TYPE_CORRUPT_FILE",
			want:  pb.ParseErrorType_PARSE_ERROR_TYPE_CORRUPT_FILE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toParseErrorType(tt.input); got != tt.want {
				t.Errorf("toParseErrorType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

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
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/bad.d2s", "bad.d2s", nil, false)

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
	if failed.ErrorType != pb.ParseErrorType_PARSE_ERROR_TYPE_CORRUPT_FILE {
		t.Errorf("parseFailed errorType = %v, want PARSE_ERROR_TYPE_CORRUPT_FILE", failed.ErrorType)
	}
}

func TestParseAndPush_FileReadError(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{files: map[string][]byte{}} // file doesn't exist
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/missing.d2s", "missing.d2s", nil, false)

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
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/test.d2s", "test.d2s", nil, false)

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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()
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
	d.allowedSaveRoots = testAllSaveRoots()
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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()
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
	d.allowedSaveRoots = testAllSaveRoots()

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
	d.allowedSaveRoots = testAllSaveRoots()
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

func TestPushSaveResult_GameRemoved_UnwatchesAndRemoves(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	cfg := d2rConfig()

	d := New(cfg, d2rFS(), watcher, d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"

	// Send PushSaveResult with GAME_REMOVED error
	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PushSaveResult{PushSaveResult: &pb.PushSaveResult{
		Error:  pb.PushSaveError_PUSH_SAVE_ERROR_GAME_REMOVED,
		GameId: "d2r",
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

	// Game should be removed from config.
	if _, ok := d.cfg.Games["d2r"]; ok {
		t.Error("d2r still in config after GAME_REMOVED")
	}
}

func TestPushSaveResult_GameRemoved_EvictsStickySaveNames(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	cfg := d2rConfig()

	d := New(cfg, d2rFS(), watcher, d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())
	d.watchedDirs["/saves/d2r"] = "d2r"
	d.lastKnownSaveNames["/saves/d2r/Hammerdin.d2s"] = "Hammerdin"
	d.lastKnownSaveNames["/saves/other/Sorceress.d2s"] = "Sorceress"

	// Send PushSaveResult with GAME_REMOVED error
	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PushSaveResult{PushSaveResult: &pb.PushSaveResult{
		Error:  pb.PushSaveError_PUSH_SAVE_ERROR_GAME_REMOVED,
		GameId: "d2r",
	}}})
	d.handleCommand(context.Background(), cmd)

	// The sticky name under the unwatched game's directory should be evicted.
	if _, ok := d.lastKnownSaveNames["/saves/d2r/Hammerdin.d2s"]; ok {
		t.Error("lastKnownSaveNames still contains /saves/d2r/Hammerdin.d2s after GAME_REMOVED")
	}

	// The sticky name under an unrelated directory should survive.
	if name, ok := d.lastKnownSaveNames["/saves/other/Sorceress.d2s"]; !ok || name != "Sorceress" {
		t.Errorf("lastKnownSaveNames[/saves/other/Sorceress.d2s] = %q, %v, want \"Sorceress\", true", name, ok)
	}
}

func TestPushSaveResult_GameRemoved_UnknownGame(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	cfg := d2rConfig()

	d := New(cfg, d2rFS(), watcher, d2rRunner(), ws, &fakePluginManager{}, nil, testLogger())

	// Send GAME_REMOVED for a game that isn't in config — should not panic
	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PushSaveResult{PushSaveResult: &pb.PushSaveResult{
		Error:  pb.PushSaveError_PUSH_SAVE_ERROR_GAME_REMOVED,
		GameId: "unknown-game",
	}}})
	d.handleCommand(context.Background(), cmd)

	// d2r should still be in config (untouched)
	if _, ok := d.cfg.Games["d2r"]; !ok {
		t.Error("d2r should still be in config")
	}

	// No directories should have been removed
	watcher.mu.Lock()
	removed := slices.Clone(watcher.removed)
	watcher.mu.Unlock()
	if len(removed) != 0 {
		t.Errorf("watcher.removed = %v, want empty", removed)
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
	d.allowedSaveRoots = testAllSaveRoots()

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

func TestDiscoverGames_StardewGlobAndPattern(t *testing.T) {
	root := t.TempDir()
	saves := filepath.Join(root, "Saves")
	gameDir := filepath.Join(saves, "Farmer_123456789")
	fsys := &fakeFS{
		dirs: map[string][]string{
			saves:   {"Farmer_123456789", "steam_autocloud.vdf"},
			gameDir: {"Farmer_123456789", "Farmer_123456789_old", "SaveGameInfo"},
		},
		files: map[string][]byte{
			filepath.Join(gameDir, "Farmer_123456789"):     {},
			filepath.Join(gameDir, "Farmer_123456789_old"): {},
			filepath.Join(gameDir, "SaveGameInfo"):         {},
			filepath.Join(saves, "steam_autocloud.vdf"):    {},
		},
	}
	pm := &fakePluginManager{manifests: map[string]pluginmgr.PluginInfo{
		"sdv": {
			GameID: "sdv", Name: "Stardew Valley",
			DefaultPaths: map[string]string{runtime.GOOS: filepath.Join(saves, "*")},
			FilePatterns: []string{"regex:^.+_[0-9]+$"},
		},
	}}
	d := New(
		Config{Games: map[string]GameConfig{}}, fsys, newFakeWatcher(), &fakeRunner{},
		newFakeWSClient(), pm, nil, testLogger(),
	)
	d.discoverGames(context.Background())
	fakeWS, ok := d.ws.(*fakeWSClient)
	if !ok {
		t.Fatal("daemon websocket is not fake client")
	}
	games := fakeWS.sentProto("gamesDiscovered", 0).GetGamesDiscovered().Games
	if len(games) != 1 || games[0].Path != filepath.Join(saves, "*") || games[0].FileCount != 1 {
		t.Fatalf("discovered games = %+v, want one glob game with fileCount 1", games)
	}
}

func TestDiscoverGames_ExistingEmptyDirectory(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{"/saves/game": {"readme.txt"}}}
	pm := &fakePluginManager{manifests: map[string]pluginmgr.PluginInfo{
		"game": {
			GameID: "game", Name: "Game",
			DefaultPaths:   map[string]string{runtime.GOOS: "/saves/game"},
			FileExtensions: []string{".sav"},
		},
	}}

	d := New(Config{Games: map[string]GameConfig{}}, fsys, newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger())
	d.discoverGames(context.Background())

	gd := ws.sentProto("gamesDiscovered", 0).GetGamesDiscovered()
	if len(gd.Games) != 0 {
		t.Fatalf("games count = %d, want 0", len(gd.Games))
	}
}

func TestDiscoverGames_EmptyFirstCandidateUsesValidSecond(t *testing.T) {
	t.Setenv("USERPROFILE", "/Users/TestUser")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	first := home + "/Documents/Game"
	second := "/Users/TestUser/Documents/Game"
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{first: {"readme.txt"}, second: {"save.sav"}}}
	pm := &fakePluginManager{manifests: map[string]pluginmgr.PluginInfo{
		"game": {
			GameID: "game", Name: "Game",
			DefaultPaths:   map[string]string{runtime.GOOS: "%DOCUMENTS%/Game"},
			FileExtensions: []string{".sav"},
		},
	}}

	d := New(Config{Games: map[string]GameConfig{}}, fsys, newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger())
	d.discoverGames(context.Background())

	gd := ws.sentProto("gamesDiscovered", 0).GetGamesDiscovered()
	if len(gd.Games) != 1 {
		t.Fatalf("games count = %d, want 1", len(gd.Games))
	}
	if gd.Games[0].Path != second || gd.Games[0].FileCount != 1 {
		t.Errorf("game = path %q, file count %d; want %q, 1", gd.Games[0].Path, gd.Games[0].FileCount, second)
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

func TestDiscoverGames_KnownFolderTemplate(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	// On Linux, resolveKnownFolder("DOCUMENTS") returns home+"/Documents".
	// Place saves there so the Known Folder candidate finds them.
	// Use a non-glob path to test the simple case clearly.
	docsPath := home + "/Documents/Paradox Interactive/Stellaris"
	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs: map[string][]string{
			docsPath: {"save.sav"},
		},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"stellaris": {
				GameID:         "stellaris",
				Name:           "Stellaris",
				DefaultPaths:   map[string]string{runtime.GOOS: "%DOCUMENTS%/Paradox Interactive/Stellaris"},
				FileExtensions: []string{".sav"},
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
	if game.GameId != "stellaris" {
		t.Errorf("gameId = %v, want stellaris", game.GameId)
	}
	if game.Path != docsPath {
		t.Errorf("path = %v, want %v", game.Path, docsPath)
	}
	if game.FileCount != 1 {
		t.Errorf("fileCount = %v, want 1", game.FileCount)
	}
}

func TestDiscoverGames_KnownFolderFallback(t *testing.T) {
	// Set USERPROFILE so the fallback candidate is generated.
	// Place saves ONLY at the fallback path, not the Known Folder path.
	t.Setenv("USERPROFILE", "/Users/TestUser")

	ws := newFakeWSClient()
	fsys := &fakeFS{
		dirs: map[string][]string{
			// Known Folder path (~/Documents/...) does NOT exist — no entry in fakeFS.
			// Fallback path (%USERPROFILE%/Documents/...) has saves.
			"/Users/TestUser/Saved Games/Diablo II Resurrected": {"Hammerdin.d2s"},
		},
		files: map[string][]byte{},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {
				GameID:         "d2r",
				Name:           "Diablo II: Resurrected",
				DefaultPaths:   map[string]string{runtime.GOOS: "%SAVED_GAMES%/Diablo II Resurrected"},
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
	if game.Path != "/Users/TestUser/Saved Games/Diablo II Resurrected" {
		t.Errorf("path = %v, want /Users/TestUser/Saved Games/Diablo II Resurrected", game.Path)
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

func TestCheckSelfUpdate_StoresAndApplyPendingTriggers(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{
		checkResult: &CheckResult{
			Daemon: &UpdateInfo{
				Version:      "0.3.0",
				URL:          "https://example.com/daemon",
				SignatureURL: "https://example.com/daemon.sig",
				SHA256:       "deadbeef",
			},
		},
	}

	cfg := Config{
		SourceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	// Phase 1: check stores but does not apply.
	d.checkSelfUpdate(context.Background())

	if v := d.PendingVersion(); v != "0.3.0" {
		t.Fatalf("PendingVersion() = %q after check, want 0.3.0", v)
	}
	updater.mu.Lock()
	if len(updater.applyCalls) != 0 {
		t.Fatal("Apply called during check phase, want deferred")
	}
	updater.mu.Unlock()

	// Phase 2: explicit apply triggers the update.
	d.ApplyPendingUpdate(context.Background())

	if !slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("missing sourceUpdateStarted event after ApplyPendingUpdate")
	}
	if !slices.Contains(ws.sentEventTypes(), "sourceOffline") {
		t.Error("missing sourceOffline after ApplyPendingUpdate")
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

	// Pending should be consumed.
	if v := d.PendingVersion(); v != "" {
		t.Errorf("PendingVersion() after apply = %q, want empty", v)
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

func TestApplyDaemonUpdate_UpdatesTrayBinary(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	cfg := Config{
		SourceID:       "deck",
		Version:        "0.1.0",
		BinaryPath:     "/usr/local/bin/savecraft-daemon",
		TrayBinaryPath: "/usr/local/bin/savecraft-tray",
		Games:          map[string]GameConfig{},
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

	result := &CheckResult{
		Daemon: &UpdateInfo{
			Version: "0.2.0",
			URL:     "https://example.com/daemon",
			SHA256:  "abc",
		},
		Tray: &UpdateInfo{
			Version: "0.2.0",
			URL:     "https://example.com/tray",
			SHA256:  "def",
		},
	}
	d.applyDaemonUpdate(context.Background(), result)

	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 2 {
		t.Fatalf("updater.Apply called %d times, want 2 (daemon + tray)", calls)
	}
	if updater.applyCalls[0].BinaryPath != "/usr/local/bin/savecraft-daemon" {
		t.Errorf("first apply binaryPath = %s, want daemon", updater.applyCalls[0].BinaryPath)
	}
	if updater.applyCalls[1].BinaryPath != "/usr/local/bin/savecraft-tray" {
		t.Errorf("second apply binaryPath = %s, want tray", updater.applyCalls[1].BinaryPath)
	}
	if exitCode != 0 {
		t.Errorf("exitFunc called with %d, want 0", exitCode)
	}
}

func TestApplyDaemonUpdate_TrayFailureDoesNotBlockDaemon(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	// Override Apply to fail only for tray path.
	cfg := Config{
		SourceID:       "deck",
		Version:        "0.1.0",
		BinaryPath:     "/usr/local/bin/savecraft-daemon",
		TrayBinaryPath: "/usr/local/bin/savecraft-tray",
		Games:          map[string]GameConfig{},
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
	var exitCalled bool
	d.exitFunc = func(code int) {
		exitCalled = true
		_ = code
	}

	// Use a custom updater that fails on tray but succeeds on daemon.
	trayFailUpdater := &trayFailFakeUpdater{}
	d.updater = trayFailUpdater

	result := &CheckResult{
		Daemon: &UpdateInfo{Version: "0.2.0", URL: "https://example.com/daemon", SHA256: "abc"},
		Tray:   &UpdateInfo{Version: "0.2.0", URL: "https://example.com/tray", SHA256: "def"},
	}
	d.applyDaemonUpdate(context.Background(), result)

	// Daemon update succeeded, so exit should still be called.
	if !exitCalled {
		t.Error("exitFunc should be called even when tray update fails")
	}
	// sourceOffline should be sent (daemon update succeeded).
	if !slices.Contains(ws.sentEventTypes(), "sourceOffline") {
		t.Error("missing sourceOffline after daemon update succeeded")
	}
}

// trayFailFakeUpdater fails Apply for tray paths, succeeds for daemon.
type trayFailFakeUpdater struct {
	applyCalls []applyCall
	mu         sync.Mutex
}

func (u *trayFailFakeUpdater) Check(_ context.Context, _, _ string) (*CheckResult, error) {
	return &CheckResult{}, nil
}

func (u *trayFailFakeUpdater) Apply(_ context.Context, info *UpdateInfo, binaryPath string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.applyCalls = append(u.applyCalls, applyCall{Info: info, BinaryPath: binaryPath})
	if binaryPath == "/usr/local/bin/savecraft-tray" {
		return fmt.Errorf("tray update failed")
	}
	return nil
}

func TestApplyDaemonUpdate_SkipsTrayWhenNoTrayPath(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	cfg := Config{
		SourceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	result := &CheckResult{
		Daemon: &UpdateInfo{Version: "0.2.0", URL: "https://example.com/daemon", SHA256: "abc"},
		Tray:   &UpdateInfo{Version: "0.2.0", URL: "https://example.com/tray", SHA256: "def"},
	}
	d.applyDaemonUpdate(context.Background(), result)

	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	// Only daemon should be applied (no TrayBinaryPath configured).
	if calls != 1 {
		t.Fatalf("updater.Apply called %d times, want 1 (daemon only, no tray path)", calls)
	}
	if exitCode != 0 {
		t.Errorf("exitFunc called with %d, want 0", exitCode)
	}
}

func TestApplyDaemonUpdate_CallsRestartFunc(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	cfg := Config{
		SourceID:       "deck",
		Version:        "0.1.0",
		BinaryPath:     "/usr/local/bin/savecraft-daemon",
		TrayBinaryPath: "/usr/local/bin/savecraft-tray",
		Games:          map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	var exitCalled bool
	d.exitFunc = func(code int) { exitCalled = true; _ = code }
	var restartDaemonPath, restartTrayPath string
	d.restartFunc = func(daemonPath, trayPath string) error {
		restartDaemonPath = daemonPath
		restartTrayPath = trayPath
		return nil
	}

	result := &CheckResult{
		Daemon: &UpdateInfo{Version: "0.2.0", URL: "https://example.com/daemon", SHA256: "abc"},
	}
	d.applyDaemonUpdate(context.Background(), result)

	if restartDaemonPath != "/usr/local/bin/savecraft-daemon" {
		t.Errorf("restartFunc daemonPath = %q, want /usr/local/bin/savecraft-daemon", restartDaemonPath)
	}
	if restartTrayPath != "/usr/local/bin/savecraft-tray" {
		t.Errorf("restartFunc trayPath = %q, want /usr/local/bin/savecraft-tray", restartTrayPath)
	}
	if !exitCalled {
		t.Error("exitFunc should be called after restart")
	}
}

func TestApplyDaemonUpdate_CallsRestartFuncWithEmptyTray(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}
	cfg := Config{
		SourceID:   "deck",
		Version:    "0.1.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	d.exitFunc = func(int) {}
	var restartTrayPath string
	d.restartFunc = func(_, trayPath string) error {
		restartTrayPath = trayPath
		return nil
	}

	result := &CheckResult{
		Daemon: &UpdateInfo{Version: "0.2.0", URL: "https://example.com/daemon", SHA256: "abc"},
	}
	d.applyDaemonUpdate(context.Background(), result)

	if restartTrayPath != "" {
		t.Errorf("restartFunc trayPath = %q, want empty string when TrayBinaryPath not set", restartTrayPath)
	}
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

	decoded, err := decodeProto(ws.sent[0])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	hb := decoded.GetSourceHeartbeat()
	ok := hb != nil
	if !ok {
		t.Fatal("missing sourceHeartbeat payload")
	}
}

// TestRun_AnnouncesOnFirstConnectSignal is the daemon-level boot-time
// resilience guard: when the connection is not available at startup, Run must
// NOT return or error and must NOT announce online — it waits. Once the
// connection comes up (the Connected() signal), it announces. This is the
// daemon-side complement to wsconn's in-process retry: a transient boot-time
// failure can never wedge or crash the daemon.
func TestRun_AnnouncesOnFirstConnectSignal(t *testing.T) {
	ws := newFakeWSClient()
	ws.manualConnect = true // connection is not available yet
	cfg := Config{
		SourceID: "deck",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{},
		ws, &fakePluginManager{}, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// While disconnected, the daemon must not announce online and must not exit.
	time.Sleep(50 * time.Millisecond)
	if slices.Contains(ws.sentEventTypes(), "sourceOnline") {
		t.Fatal("announced online before any connection was established")
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned before connecting (err=%v) — initial outage must not be fatal", err)
	default:
	}

	// Connection comes up — daemon announces online.
	ws.connected <- struct{}{}
	waitFor(t, func() bool {
		return slices.Contains(ws.sentEventTypes(), "sourceOnline")
	})

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned %v, want nil on graceful shutdown", err)
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
	ws.connected <- struct{}{}

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

	// On reconnect with unchanged games/files, only sourceOnline should be
	// re-sent. All discovery, scan, watch, parse, and push messages should
	// be suppressed because nothing changed.
	for _, suppressed := range []string{
		"gamesDiscovered", "scanStarted", "scanCompleted",
		"gameDetected", "watching", "parseStarted", "parseCompleted", "pushSave",
	} {
		count := countEventType(ws, suppressed)
		if count != 1 {
			t.Errorf("%s sent %d times, want 1 (should not re-send on reconnect)", suppressed, count)
		}
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

	ws.isConnected = true

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

	ws.isConnected = true

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
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

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
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

	pushCount1 := countEventType(ws, "pushSave")
	if pushCount1 != 1 {
		t.Fatalf("after first parse: pushSave count = %d, want 1", pushCount1)
	}

	// Second parse with identical output — should skip push.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

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
	runner := &fakeRunner{
		results: map[string]*GameState{"d2r": state1},
	}
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

	// Change the runner output to simulate leveling up.
	runner.mu.Lock()
	runner.results["d2r"] = state2
	runner.mu.Unlock()

	// Second parse with different output — should push.
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

	pushCount := countEventType(ws, "pushSave")
	if pushCount != 2 {
		t.Errorf("pushSave count = %d, want 2 (both should push since output changed)", pushCount)
	}
}

func TestParseAndPush_HashUpdatedOnlyAfterSuccessfulPush(t *testing.T) {
	t.Run("caches section hashes after successful send", func(t *testing.T) {
		ws := newFakeWSClient()
		runner := d2rRunner()
		fsys := d2rFS()
		cfg := d2rConfig()

		d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

		d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

		if len(d.lastPushedSectionHashes) != 1 {
			t.Fatalf("lastPushedSectionHashes has %d entries, want 1", len(d.lastPushedSectionHashes))
		}
		cached, ok := d.lastPushedSectionHashes["/saves/d2r/Hammerdin.d2s"]
		if !ok {
			t.Fatal("lastPushedSectionHashes missing entry for /saves/d2r/Hammerdin.d2s")
		}
		if len(cached.hashes) == 0 {
			t.Error("section hashes should not be empty")
		}
	})

	t.Run("does not cache hash when send fails", func(t *testing.T) {
		ws := newFakeWSClient()
		ws.sendErr = fmt.Errorf("connection lost")
		runner := d2rRunner()
		fsys := d2rFS()
		cfg := d2rConfig()

		d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

		d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

		if len(d.lastPushedSectionHashes) != 0 {
			t.Errorf("lastPushedSectionHashes has %d entries, want 0 (send failed, should not cache)",
				len(d.lastPushedSectionHashes))
		}
	})
}

func TestParseAndPush_OnlyChangedSectionsSent(t *testing.T) {
	ws := newFakeWSClient()

	state1 := &GameState{
		Identity: Identity{SaveName: "Player", GameID: "magic"},
		Summary:  "Player, Gold 4",
		Sections: map[string]Section{
			"decks":    {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
			"rank":     {Description: "Rank info", Data: jsontext.Value(`{"class":"Gold"}`)},
			"game_log": {Description: "Game log", Data: jsontext.Value(`{"games":1}`)},
		},
	}
	// State 2: only game_log changed.
	state2 := &GameState{
		Identity: Identity{SaveName: "Player", GameID: "magic"},
		Summary:  "Player, Gold 4",
		Sections: map[string]Section{
			"decks":    {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
			"rank":     {Description: "Rank info", Data: jsontext.Value(`{"class":"Gold"}`)},
			"game_log": {Description: "Game log", Data: jsontext.Value(`{"games":2}`)},
		},
	}

	runner := &fakeRunner{results: map[string]*GameState{"magic": state1}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First push — all 3 sections should be sent.
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push1 := ws.sentProto("pushSave", 0)
	if push1 == nil {
		t.Fatal("first push not sent")
	}
	ps1 := push1.GetPushSave()
	if len(ps1.Sections) != 3 {
		t.Errorf("first push: got %d sections, want 3", len(ps1.Sections))
	}

	// Change runner output so only game_log differs.
	runner.mu.Lock()
	runner.results["magic"] = state2
	runner.mu.Unlock()

	// Second push — only game_log should be sent.
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push2 := ws.sentProto("pushSave", 1)
	if push2 == nil {
		t.Fatal("second push not sent")
	}
	ps2 := push2.GetPushSave()
	if len(ps2.Sections) != 1 {
		t.Errorf("second push: got %d sections, want 1 (only changed)", len(ps2.Sections))
	}
	if ps2.Sections[0].Name != "game_log" {
		t.Errorf("second push: section name = %q, want %q", ps2.Sections[0].Name, "game_log")
	}

	// AllSectionNames should always contain all 3 names, even on partial push.
	allNames := ps2.GetAllSectionNames()
	if len(allNames) != 3 {
		t.Errorf("second push: AllSectionNames has %d entries, want 3", len(allNames))
	}
	wantNames := map[string]bool{"decks": true, "game_log": true, "rank": true}
	for _, n := range allNames {
		if !wantNames[n] {
			t.Errorf("unexpected section name in AllSectionNames: %q", n)
		}
	}
}

// TestParseAndPush_CrossIdentityDelta_FullPushOnIdentityChange guards against
// the delta cache being computed across a save-identity change on the same
// file path. In production, MTGA log rotation causes a boot-only log to
// parse to the fallback identity "Unknown Player", and the very next parse
// of the same path (once the real session log is written) to parse back to
// the actual player name. If the cache is scoped to file path alone, the
// second parse's sections are diffed against the first identity's hashes;
// any section whose content happens to match is dropped from the push, and
// the server creates/updates a save from that partial payload — permanently
// missing every section that was hash-stable across the identity change.
func TestParseAndPush_CrossIdentityDelta_FullPushOnIdentityChange(t *testing.T) {
	ws := newFakeWSClient()

	sections := map[string]Section{
		"decks":    {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
		"rank":     {Description: "Rank info", Data: jsontext.Value(`{"class":"Gold"}`)},
		"game_log": {Description: "Game log", Data: jsontext.Value(`{"games":1}`)},
	}
	stateA := &GameState{
		Identity: Identity{SaveName: "Player A", GameID: "magic"},
		Summary:  "Player A, Gold 4",
		Sections: sections,
	}
	// Same section content as stateA, but a different save identity —
	// simulates log rotation parsing the same file to a different player.
	stateB := &GameState{
		Identity: Identity{SaveName: "Player B", GameID: "magic"},
		Summary:  "Player B, Gold 4",
		Sections: sections,
	}

	runner := &fakeRunner{results: map[string]*GameState{"magic": stateA}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse — identity "Player A" — full push.
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push1 := ws.sentProto("pushSave", 0)
	if push1 == nil {
		t.Fatal("first push not sent")
	}
	if got := len(push1.GetPushSave().Sections); got != 3 {
		t.Fatalf("first push: got %d sections, want 3", got)
	}

	// Same file path, plugin now reports a different save identity with
	// byte-identical section content.
	runner.mu.Lock()
	runner.results["magic"] = stateB
	runner.mu.Unlock()

	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push2 := ws.sentProto("pushSave", 1)
	if push2 == nil {
		t.Fatal("second push not sent (cross-identity parse must not be treated as a no-op)")
	}
	ps2 := push2.GetPushSave()
	if got := len(ps2.Sections); got != 3 {
		t.Errorf("second push (identity changed A->B): got %d sections, want 3 "+
			"(full push — delta cache must be cold across a save-identity change)", got)
	}
}

// TestParseAndPush_IdentityFlap_RescopesCacheOnReturn covers the case where
// identity flaps back to a previously-seen value (A -> B -> A). The third
// push must again be a full push: the cache was re-scoped to B by the
// second parse, so the stale A hashes must not be reused even though A was
// seen before.
func TestParseAndPush_IdentityFlap_RescopesCacheOnReturn(t *testing.T) {
	ws := newFakeWSClient()

	sections := map[string]Section{
		"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
		"rank":  {Description: "Rank info", Data: jsontext.Value(`{"class":"Gold"}`)},
	}
	stateA := &GameState{Identity: Identity{SaveName: "Player A", GameID: "magic"}, Summary: "A", Sections: sections}
	stateB := &GameState{Identity: Identity{SaveName: "Player B", GameID: "magic"}, Summary: "B", Sections: sections}

	runner := &fakeRunner{results: map[string]*GameState{"magic": stateA}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// A: full push.
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	// B: identity changed, cache must be cold, full push.
	runner.mu.Lock()
	runner.results["magic"] = stateB
	runner.mu.Unlock()
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	// Back to A with unchanged content — the cache was re-scoped to B in
	// between, so this must be a full push again, not a delta or a skip
	// against the stale A hashes.
	runner.mu.Lock()
	runner.results["magic"] = stateA
	runner.mu.Unlock()
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push3 := ws.sentProto("pushSave", 2)
	if push3 == nil {
		t.Fatal("third push not sent (identity flap back to A must not be treated as a no-op)")
	}
	if got := len(push3.GetPushSave().Sections); got != 2 {
		t.Errorf("third push (identity flapped A->B->A): got %d sections, want 2 (full push)", got)
	}
}

// TestParseAndPush_StickyIdentity_EmptySaveNameDeltasAgainstLastKnownName
// guards the daemon-side half of the identity-stickiness fix: MTGA's
// Player.log sometimes has no login event yet (a boot-only log after a
// client restart), so the plugin emits an empty saveName ("identity
// unknown" — see plugins/magic/parser.resolveSaveName). The daemon must
// substitute the last-known name for the file path BEFORE the
// identity-scoped delta cache (filterChangedSections) is consulted, so the
// boot-only re-parse deltas against the same save instead of forking a new
// "Unknown Player" save.
func TestParseAndPush_StickyIdentity_EmptySaveNameDeltasAgainstLastKnownName(t *testing.T) {
	ws := newFakeWSClient()

	named := &GameState{
		Identity: Identity{SaveName: "Player A", GameID: "magic"},
		Summary:  "Player A, Gold 4",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
			"rank":  {Description: "Rank info", Data: jsontext.Value(`{"class":"Gold"}`)},
		},
	}

	runner := &fakeRunner{results: map[string]*GameState{"magic": named}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse — named identity — full push.
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)
	push1 := ws.sentProto("pushSave", 0)
	if push1 == nil {
		t.Fatal("first push not sent")
	}
	if got := len(push1.GetPushSave().Sections); got != 2 {
		t.Fatalf("first push: got %d sections, want 2", got)
	}

	// Boot-only re-parse of the same path: empty saveName, one section changed.
	runner.mu.Lock()
	runner.results["magic"] = &GameState{
		Identity: Identity{SaveName: "", GameID: "magic"},
		Summary:  "Player A, Platinum 1",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
			"rank":  {Description: "Rank info", Data: jsontext.Value(`{"class":"Platinum"}`)}, // changed
		},
	}
	runner.mu.Unlock()

	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push2 := ws.sentProto("pushSave", 1)
	if push2 == nil {
		t.Fatal("second push not sent")
	}
	ps2 := push2.GetPushSave()
	if ps2.Identity.Name != "Player A" {
		t.Errorf("second push saveName = %q, want %q (sticky substitution)", ps2.Identity.Name, "Player A")
	}
	if got := len(ps2.Sections); got != 1 {
		t.Fatalf("second push: got %d sections, want 1 (delta against the same save, not a fork)", got)
	}
	if ps2.Sections[0].Name != "rank" {
		t.Errorf("second push changed section = %q, want %q", ps2.Sections[0].Name, "rank")
	}
}

// TestParseAndPush_EmptySaveNameFallsBackToUnknownPlayer covers the case
// where the daemon has never seen a name for a file path (e.g. the very
// first Player.log parse happens to be boot-only). With no sticky name to
// substitute, the daemon falls back to "Unknown Player" — the literal the
// plugin used to emit itself, now owned by the daemon.
func TestParseAndPush_EmptySaveNameFallsBackToUnknownPlayer(t *testing.T) {
	ws := newFakeWSClient()
	state := &GameState{
		Identity: Identity{SaveName: "", GameID: "magic"},
		Summary:  "MTG Arena Player",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
		},
	}
	runner := &fakeRunner{results: map[string]*GameState{"magic": state}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)

	push := ws.sentProto("pushSave", 0)
	if push == nil {
		t.Fatal("push not sent")
	}
	if got := push.GetPushSave().Identity.Name; got != "Unknown Player" {
		t.Errorf("pushed saveName = %q, want %q", got, "Unknown Player")
	}
	pc := ws.sentProto("parseCompleted", 0)
	if pc == nil {
		t.Fatal("parseCompleted not sent")
	}
	if got := pc.GetParseCompleted().Identity.Name; got != "Unknown Player" {
		t.Errorf("parseCompleted saveName = %q, want %q (must match the push, resolved once)", got, "Unknown Player")
	}
}

// TestParseAndPush_StickyIdentity_UpdatesAfterNamedParseReturns covers the
// (no name) -> "Unknown Player" -> named -> (no name) sequence. Once a named
// parse returns for a path previously stuck on the "Unknown Player"
// fallback, the remembered name must update, so a later boot-only re-parse
// substitutes the real name rather than replaying the stale fallback.
func TestParseAndPush_StickyIdentity_UpdatesAfterNamedParseReturns(t *testing.T) {
	ws := newFakeWSClient()

	emptyState := &GameState{
		Identity: Identity{SaveName: "", GameID: "magic"},
		Summary:  "MTG Arena Player",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
		},
	}
	namedState := &GameState{
		Identity: Identity{SaveName: "Player A", GameID: "magic"},
		Summary:  "Player A",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":80}`)},
		},
	}

	runner := &fakeRunner{results: map[string]*GameState{"magic": emptyState}}
	cfg := Config{
		SourceID: "test-source",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"magic": {SavePath: "/saves/mtga", FileExtensions: []string{".log"}, Enabled: true},
		},
	}
	fsys := &fakeFS{files: map[string][]byte{"/saves/mtga/Player.log": []byte("data")}}

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())

	// First parse: no prior name — falls back to "Unknown Player".
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)
	push1 := ws.sentProto("pushSave", 0)
	if push1 == nil || push1.GetPushSave().Identity.Name != "Unknown Player" {
		t.Fatalf("first push saveName = %v, want %q", push1, "Unknown Player")
	}

	// Second parse: real identity resolves — full push (identity change,
	// wave 1 behavior), and the resolved name becomes the new sticky name.
	runner.mu.Lock()
	runner.results["magic"] = namedState
	runner.mu.Unlock()
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)
	push2 := ws.sentProto("pushSave", 1)
	if push2 == nil {
		t.Fatal("second push not sent")
	}
	if got := push2.GetPushSave().Identity.Name; got != "Player A" {
		t.Fatalf("second push saveName = %q, want %q", got, "Player A")
	}
	if got := len(push2.GetPushSave().Sections); got != 1 {
		t.Fatalf("second push: got %d sections, want 1 (full push on identity change)", got)
	}

	// Third parse: boot-only re-parse (empty saveName again) with one
	// changed section. Must substitute "Player A" — the most recently seen
	// real name — NOT the stale "Unknown Player" fallback from the first
	// parse. Substituting the stale fallback would mismatch the cache's
	// saveName ("Player A"), forcing a spurious full push under the wrong
	// identity instead of this delta.
	runner.mu.Lock()
	runner.results["magic"] = &GameState{
		Identity: Identity{SaveName: "", GameID: "magic"},
		Summary:  "Player A",
		Sections: map[string]Section{
			"decks": {Description: "Deck lists", Data: jsontext.Value(`{"count":81}`)}, // changed
		},
	}
	runner.mu.Unlock()
	d.parseAndPush(context.Background(), "magic", "/saves/mtga/Player.log", "Player.log", nil, false)
	push3 := ws.sentProto("pushSave", 2)
	if push3 == nil {
		t.Fatal("third push not sent")
	}
	ps3 := push3.GetPushSave()
	if ps3.Identity.Name != "Player A" {
		t.Errorf("third push saveName = %q, want %q (sticky name must update after the named parse)",
			ps3.Identity.Name, "Player A")
	}
	if got := len(ps3.Sections); got != 1 {
		t.Errorf("third push: got %d sections, want 1 (delta — cache must stay warm under Player A)", got)
	}
}

func TestSentMessagesAreGzipCompressed(t *testing.T) {
	ws := newFakeWSClient()
	runner := d2rRunner()
	fsys := d2rFS()
	cfg := d2rConfig()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, &fakePluginManager{}, nil, testLogger())
	d.parseAndPush(context.Background(), "d2r", "/saves/d2r/Hammerdin.d2s", "Hammerdin.d2s", nil, false)

	ws.mu.Lock()
	defer ws.mu.Unlock()

	for i, data := range ws.sent {
		if len(data) < 2 {
			t.Errorf("message %d is too short (%d bytes)", i, len(data))
			continue
		}
		if data[0] != 0x1f || data[1] != 0x8b {
			t.Errorf("message %d is not gzip-compressed (magic bytes: %#x %#x)", i, data[0], data[1])
		}
	}
}

func TestUpdatePlugins_Success(t *testing.T) {
	ws := newFakeWSClient()
	pm := &fakePluginManager{updateResult: []string{"d2r"}}
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, pm, nil, testLogger())

	updated, err := dmn.UpdatePlugins(context.Background())
	if err != nil {
		t.Fatalf("UpdatePlugins() error: %v", err)
	}
	if len(updated) != 1 || updated[0] != "d2r" {
		t.Errorf("updated = %v, want [d2r]", updated)
	}

	// Verify PluginUpdated message was sent.
	types := ws.sentEventTypes()
	if !slices.Contains(types, "pluginUpdated") {
		t.Errorf("expected pluginUpdated message, got %v", types)
	}

	// Verify reset channel was signaled.
	select {
	case <-dmn.pluginUpdateResetCh:
		// ok
	default:
		t.Error("expected pluginUpdateResetCh to be signaled")
	}
}

func TestPluginChangeInvalidatesOnlyGameAndRepushes(t *testing.T) {
	ws := newFakeWSClient()
	ws.isConnected = true
	state := &GameState{Identity: Identity{GameID: "d2r", SaveName: "Hero"}, Sections: map[string]Section{
		"header":    {Data: jsontext.Value(`{"level":1}`)},
		"inventory": {Data: jsontext.Value(`{"items":2}`)},
	}}
	runner := &fakeRunner{results: map[string]*GameState{"d2r": state}}
	fsys := &fakeFS{
		files: map[string][]byte{"/saves/Hero.d2s": []byte("save")},
		dirs:  map[string][]string{"/saves": {"Hero.d2s"}},
	}
	pm := &fakePluginManager{updateResult: []string{"d2r"}}
	d := New(Config{Games: map[string]GameConfig{
		"d2r": {SavePath: "/saves", FileExtensions: []string{".d2s"}},
	}}, fsys, newFakeWatcher(), runner, ws, pm, nil, testLogger())
	d.watchedDirs["/saves"] = "d2r"
	d.pushState(context.Background(), "d2r", "/saves/Hero.d2s", state)
	if got := countEventType(ws, "pushSave"); got != 1 {
		t.Fatalf("initial pushSave count = %d, want 1", got)
	}
	d.lastPushedSectionHashes["/other/save"] = &sectionHashCache{
		gameID: "other", saveName: "Other", hashes: map[string][32]byte{"x": {1}},
	}
	d.parseFailures["/saves/Hero.d2s"] = parseFailure{gameID: "d2r"}
	d.parseFailures["/other/save"] = parseFailure{gameID: "other"}
	if _, err := d.UpdatePlugins(context.Background()); err != nil {
		t.Fatalf("UpdatePlugins() error: %v", err)
	}
	if _, ok := d.lastPushedSectionHashes["/saves/Hero.d2s"]; !ok {
		t.Fatal("expected rescanned save to repopulate its cache")
	}
	if _, ok := d.lastPushedSectionHashes["/other/save"]; !ok {
		t.Fatal("plugin change deleted another game's cache")
	}
	if got := countEventType(ws, "pushSave"); got != 2 {
		t.Fatalf("pushSave count after plugin update = %d, want 2", got)
	}
	second := ws.sentProto("pushSave", 1).GetPushSave()
	if len(second.Sections) != 2 || len(second.AllSectionNames) != 2 {
		t.Fatalf("plugin repush sections = %d names = %d, want 2/2", len(second.Sections), len(second.AllSectionNames))
	}
	if _, ok := d.parseFailures["/saves/Hero.d2s"]; ok {
		t.Fatal("plugin change retained changed game's parse failure")
	}
	if _, ok := d.parseFailures["/other/save"]; !ok {
		t.Fatal("plugin change deleted another game's parse failure")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("rescan runner calls = %d, want 1", len(runner.calls))
	}
	d.lastPushedSectionHashes["/saves/Hero.d2s"] = &sectionHashCache{gameID: "d2r"}
	delete(d.watchedDirs, "/saves")
	d.handlePluginAvailable(context.Background(), &pb.PluginAvailable{GameId: "d2r"})
	if _, ok := d.lastPushedSectionHashes["/saves/Hero.d2s"]; ok {
		t.Fatal("server-notified plugin change retained cache entry")
	}
}

func TestUnwatchGameEvictsParseFailures(t *testing.T) {
	d := New(
		Config{}, &fakeFS{dirs: map[string][]string{"/saves": {"a.sav"}}},
		newFakeWatcher(), &fakeRunner{}, newFakeWSClient(), nil, nil, testLogger(),
	)
	d.parseFailures["/saves/a.sav"] = parseFailure{gameID: "game"}
	d.lastPushedSectionHashes["/saves/a.sav"] = &sectionHashCache{gameID: "game"}
	d.unwatchGame(context.Background(), "/saves")
	if _, ok := d.parseFailures["/saves/a.sav"]; ok {
		t.Fatal("unwatch retained parse failure")
	}
}

// TestUnwatchGame_DirectoryUnit_EvictsSnapshotHashAndRedispatches closes the
// Q3 coverage gap: scanning a palworld-style directory-unit fixture seeds
// dirUnitSnapshotHashes for the resolved save directory (see
// dispatchDirectoryUnit); unwatchGame must evict that entry
// (evictUnwatchedPathCaches already handles dirUnitSnapshotHashes, but
// nothing exercised the directory-unit path specifically), and a later
// re-scan with byte-identical on-disk content must still re-dispatch, since
// the aggregate dedup has nothing left to compare against.
func TestUnwatchGame_DirectoryUnit_EvictsSnapshotHashAndRedispatches(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"palworld": newD2RState()}}
	cfg := palworldConfig()
	pm := palworldPluginManager()

	d := New(cfg, palworldFS(), newFakeWatcher(), runner, ws, pm, nil, testLogger())
	d.scanGame(context.Background(), "palworld", cfg.Games["palworld"], false)
	if len(runner.calls) != 1 {
		t.Fatalf("setup: runner called %d times, want 1", len(runner.calls))
	}

	d.mu.RLock()
	_, seeded := d.dirUnitSnapshotHashes[palworldSaveDir]
	d.mu.RUnlock()
	if !seeded {
		t.Fatal("setup: dirUnitSnapshotHashes not seeded by the initial scan")
	}

	d.unwatchGame(context.Background(), cfg.Games["palworld"].SavePath)

	d.mu.RLock()
	_, stillThere := d.dirUnitSnapshotHashes[palworldSaveDir]
	d.mu.RUnlock()
	if stillThere {
		t.Error("unwatchGame did not evict the directory-unit snapshot hash")
	}

	d.scanGame(context.Background(), "palworld", cfg.Games["palworld"], false)
	if len(runner.calls) != 2 {
		t.Errorf(
			"runner called %d times after re-scan, want 2 (evicted snapshot must force a re-dispatch)",
			len(runner.calls),
		)
	}
}

func TestUpdatePlugins_NoPluginManager(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, nil, nil, testLogger())

	_, err := dmn.UpdatePlugins(context.Background())
	if err == nil {
		t.Fatal("expected error when plugin manager is nil")
	}
}

func TestUpdatePlugins_CheckError(t *testing.T) {
	ws := newFakeWSClient()
	pm := &fakePluginManager{updateErr: fmt.Errorf("manifest fetch failed")}
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, pm, nil, testLogger())

	_, err := dmn.UpdatePlugins(context.Background())
	if err == nil {
		t.Fatal("expected error from CheckForUpdates")
	}
}

func TestUpdatePlugins_NoneUpdated(t *testing.T) {
	ws := newFakeWSClient()
	pm := &fakePluginManager{updateResult: nil}
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, pm, nil, testLogger())

	updated, err := dmn.UpdatePlugins(context.Background())
	if err != nil {
		t.Fatalf("UpdatePlugins() error: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %v, want empty", updated)
	}

	// No PluginUpdated message should be sent.
	types := ws.sentEventTypes()
	for _, typ := range types {
		if typ == "pluginUpdated" {
			t.Error("unexpected pluginUpdated message when nothing was updated")
		}
	}
}

func TestHandlePluginAvailable_Success(t *testing.T) {
	ws := newFakeWSClient()
	pm := &fakePluginManager{}
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, pm, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PluginAvailable{PluginAvailable: &pb.PluginAvailable{
		GameId:  "d2r",
		Version: "1.2.0",
	}}})
	dmn.handleCommand(context.Background(), cmd)

	// Verify EnsurePlugin was called for the right game.
	pm.mu.Lock()
	ensured := slices.Clone(pm.ensured)
	pm.mu.Unlock()
	if len(ensured) != 1 || ensured[0] != "d2r" {
		t.Errorf("ensured = %v, want [d2r]", ensured)
	}

	// Verify PluginUpdated was sent.
	if !slices.Contains(ws.sentEventTypes(), "pluginUpdated") {
		t.Errorf("expected pluginUpdated, got %v", ws.sentEventTypes())
	}

	// Verify timer reset was signaled.
	select {
	case <-dmn.pluginUpdateResetCh:
	default:
		t.Error("expected pluginUpdateResetCh to be signaled")
	}
}

func TestHandlePluginAvailable_EnsureError(t *testing.T) {
	ws := newFakeWSClient()
	pm := &fakePluginManager{
		ensureErr: map[string]error{"d2r": fmt.Errorf("download failed")},
	}
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, pm, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PluginAvailable{PluginAvailable: &pb.PluginAvailable{
		GameId:  "d2r",
		Version: "1.2.0",
	}}})
	dmn.handleCommand(context.Background(), cmd)

	// Verify PluginDownloadFailed was sent.
	if !slices.Contains(ws.sentEventTypes(), "pluginDownloadFailed") {
		t.Errorf("expected pluginDownloadFailed, got %v", ws.sentEventTypes())
	}

	// Should NOT have sent PluginUpdated.
	if slices.Contains(ws.sentEventTypes(), "pluginUpdated") {
		t.Error("unexpected pluginUpdated on failure")
	}
}

func TestHandlePluginAvailable_NoPluginManager(t *testing.T) {
	ws := newFakeWSClient()
	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}
	dmn := New(cfg, &fakeFS{}, &fakeWatcher{events: make(chan FileEvent)}, nil, ws, nil, nil, testLogger())

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_PluginAvailable{PluginAvailable: &pb.PluginAvailable{
		GameId:  "d2r",
		Version: "1.2.0",
	}}})
	// Should not panic with nil plugin manager, and no messages sent.
	dmn.handleCommand(context.Background(), cmd)

	if len(ws.sentEventTypes()) != 0 {
		t.Errorf("expected no messages sent, got %v", ws.sentEventTypes())
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

func TestPluginReloadChannel_ReloadsAndRescans(t *testing.T) {
	ws := newFakeWSClient()
	ws.isConnected = true

	runner := &fakeRunner{
		results: map[string]*GameState{
			"d2r": {
				Identity: Identity{SaveName: "Hero", GameID: "d2r"},
				Summary:  "Test Hero",
				Sections: map[string]Section{
					"header": {
						Description: "Header",
						Data:        jsontext.Value(`{"level":1}`),
					},
				},
			},
		},
	}

	fsys := &fakeFS{
		files: map[string][]byte{
			"/saves/d2r/Hero.d2s": []byte("save data"),
		},
		dirs: map[string][]string{
			"/saves/d2r": {"Hero.d2s"},
		},
	}

	pm := &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"d2r": {GameID: "d2r", Name: "Diablo II: Resurrected"},
		},
	}

	cfg := Config{
		SourceID:   "test",
		SourceUUID: "test-uuid",
		Version:    "0.1.0",
		Games: map[string]GameConfig{
			"d2r": {
				SavePath:       "/saves/d2r",
				FileExtensions: []string{".d2s"},
				Enabled:        true,
			},
		},
	}

	watcher := newFakeWatcher()
	dmn := New(cfg, fsys, watcher, runner, ws, pm, nil, testLogger())

	reloadCh := make(chan string, 1)
	dmn.SetPluginReloadCh(reloadCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the daemon in a goroutine.
	errCh := make(chan error, 1)
	go func() { errCh <- dmn.Run(ctx) }()

	// Wait for daemon to start (it sends SourceOnline).
	waitFor(t, func() bool { return len(ws.sentEventTypes()) > 0 })

	// Send plugin reload event.
	reloadCh <- "d2r"

	// Wait for EnsurePlugin to be called at least twice (startup + reload).
	d2rEnsureCount := func() int {
		pm.mu.Lock()
		defer pm.mu.Unlock()
		count := 0
		for _, g := range pm.ensured {
			if g == "d2r" {
				count++
			}
		}
		return count
	}
	waitFor(t, func() bool { return d2rEnsureCount() >= 2 })

	// Verify runner was called (re-parse after reload).
	d2rRunCount := func() int {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		count := 0
		for _, c := range runner.calls {
			if c.GameID == "d2r" {
				count++
			}
		}
		return count
	}
	// At least 2: initial scan + re-parse after reload.
	waitFor(t, func() bool { return d2rRunCount() >= 2 })

	cancel()
	<-errCh
}

func TestPluginReloadChannel_NilDoesNotPanic(t *testing.T) {
	// When no pluginReloadCh is set, the daemon should work normally.
	ws := newFakeWSClient()
	ws.isConnected = true

	cfg := Config{
		SourceID: "test",
		Version:  "0.1.0",
		Games:    map[string]GameConfig{},
	}

	dmn := New(
		cfg, &fakeFS{dirs: map[string][]string{}},
		newFakeWatcher(), nil, ws, nil, nil, testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- dmn.Run(ctx) }()

	waitFor(t, func() bool { return len(ws.sentEventTypes()) > 0 })
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestCheckSelfUpdate_StoresPendingUpdate(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{
		checkResult: &CheckResult{
			Daemon: &UpdateInfo{
				Version:      "0.3.0",
				URL:          "https://example.com/daemon",
				SignatureURL: "https://example.com/daemon.sig",
				SHA256:       "deadbeef",
			},
		},
	}

	cfg := Config{
		SourceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	d.exitFunc = func(_ int) {}

	d.checkSelfUpdate(context.Background())

	// Should store result, NOT immediately apply.
	if v := d.PendingVersion(); v != "0.3.0" {
		t.Errorf("PendingVersion() = %q, want %q", v, "0.3.0")
	}

	// Should NOT have sent sourceUpdateStarted (no immediate apply).
	if slices.Contains(ws.sentEventTypes(), "sourceUpdateStarted") {
		t.Error("should not send sourceUpdateStarted on check (deferred apply)")
	}

	// Updater.Apply should NOT have been called.
	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 0 {
		t.Errorf("updater.Apply called %d times, want 0 (deferred)", calls)
	}
}

func TestPendingVersion_EmptyWhenNoUpdate(t *testing.T) {
	d := New(
		Config{SourceID: "deck", Version: "0.2.0", Games: map[string]GameConfig{}},
		&fakeFS{}, newFakeWatcher(), &fakeRunner{}, newFakeWSClient(),
		&fakePluginManager{}, nil, testLogger(),
	)

	if v := d.PendingVersion(); v != "" {
		t.Errorf("PendingVersion() = %q, want empty", v)
	}
}

func TestApplyPendingUpdate_ConsumesStoredResult(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}

	cfg := Config{
		SourceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	var exitCode int
	d.exitFunc = func(code int) { exitCode = code }

	// Store a pending update.
	d.StorePendingUpdate(&CheckResult{
		Daemon: &UpdateInfo{
			Version:      "0.3.0",
			URL:          "https://example.com/daemon",
			SignatureURL: "https://example.com/daemon.sig",
			SHA256:       "deadbeef",
		},
	})

	d.ApplyPendingUpdate(context.Background())

	// Should have applied.
	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 1 {
		t.Fatalf("updater.Apply called %d times, want 1", calls)
	}
	if updater.applyCalls[0].Info.Version != "0.3.0" {
		t.Errorf("version = %s, want 0.3.0", updater.applyCalls[0].Info.Version)
	}

	// Should have consumed the pending update.
	if v := d.PendingVersion(); v != "" {
		t.Errorf("PendingVersion() after apply = %q, want empty", v)
	}

	if exitCode != 0 {
		t.Errorf("exitFunc called with %d, want 0", exitCode)
	}
}

func TestApplyPendingUpdate_NoopWhenNoPending(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{}

	d := New(
		Config{SourceID: "deck", Version: "0.2.0", Games: map[string]GameConfig{}},
		&fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws,
		&fakePluginManager{}, updater, testLogger(),
	)
	d.exitFunc = func(_ int) {}

	// Call with no pending update — should be a no-op.
	d.ApplyPendingUpdate(context.Background())

	updater.mu.Lock()
	calls := len(updater.applyCalls)
	updater.mu.Unlock()
	if calls != 0 {
		t.Errorf("updater.Apply called %d times, want 0", calls)
	}
}

func TestCheckSelfUpdate_AutoApplyTimerFires(t *testing.T) {
	// Shorten the grace period for testing.
	orig := autoApplyGracePeriod
	autoApplyGracePeriod = 50 * time.Millisecond
	t.Cleanup(func() { autoApplyGracePeriod = orig })

	ws := newFakeWSClient()
	updater := &fakeUpdater{
		checkResult: &CheckResult{
			Daemon: &UpdateInfo{
				Version:      "0.4.0",
				URL:          "https://example.com/daemon",
				SignatureURL: "https://example.com/daemon.sig",
				SHA256:       "deadbeef",
			},
		},
	}

	cfg := Config{
		SourceID:   "deck",
		Version:    "0.2.0",
		BinaryPath: "/usr/local/bin/savecraft-daemon",
		Games:      map[string]GameConfig{},
	}

	d := New(cfg, &fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, updater, testLogger())
	d.exitFunc = func(_ int) {}

	d.checkSelfUpdate(context.Background())

	// Pending update should be stored immediately.
	if v := d.PendingVersion(); v != "0.4.0" {
		t.Fatalf("PendingVersion() = %q, want 0.4.0", v)
	}

	// Wait for the auto-apply timer to fire and Apply to be called.
	// We wait on applyCalls rather than PendingVersion because PendingVersion
	// clears before Apply is invoked, causing a race.
	waitFor(t, func() bool {
		updater.mu.Lock()
		n := len(updater.applyCalls)
		updater.mu.Unlock()
		return n == 1
	})
	if updater.applyCalls[0].Info.Version != "0.4.0" {
		t.Errorf("applied version = %s, want 0.4.0", updater.applyCalls[0].Info.Version)
	}
}

func TestCheckSelfUpdate_NoTimerWhenNoUpdate(t *testing.T) {
	ws := newFakeWSClient()
	updater := &fakeUpdater{checkResult: nil}

	d := New(
		Config{SourceID: "deck", Version: "0.2.0", Games: map[string]GameConfig{}},
		&fakeFS{}, newFakeWatcher(), &fakeRunner{}, ws,
		&fakePluginManager{}, updater, testLogger(),
	)

	d.checkSelfUpdate(context.Background())

	// No update detected — PendingVersion should be empty and no timer scheduled.
	if v := d.PendingVersion(); v != "" {
		t.Errorf("PendingVersion() = %q, want empty", v)
	}

	d.mu.RLock()
	hasTimer := d.autoApplyTimer != nil
	d.mu.RUnlock()
	if hasTimer {
		t.Error("autoApplyTimer should be nil when no update detected")
	}
}

// --- Tests: save-path allowlist (finding 4.3 / epic R12) ---

func TestSaveRootAllowed(t *testing.T) {
	fsys := &fakeFS{
		dirs: map[string][]string{
			"/home/u":          {},
			"/home/u/saves":    {},
			"/home/u/evillink": {},
			"/etc":             {},
		},
		symlinks: map[string]string{
			"/home/u/evillink": "/etc", // a save dir that symlinks out
		},
	}
	d := New(
		d2rConfig(), fsys, newFakeWatcher(), &fakeRunner{},
		newFakeWSClient(), &fakePluginManager{}, nil, testLogger(),
	)
	d.allowedSaveRoots = []string{"/home/u"}

	tests := []struct {
		path string
		want bool
	}{
		{"/home/u", true},
		{"/home/u/saves", true},
		{"/home/u/new/not/yet/created", true}, // in-root, not-yet-existing
		{"/etc", false},
		{"/etc/passwd", false},
		{"/home/u-evil", false}, // prefix-trap: must NOT match /home/u
		{"/home/u-evil/x", false},
		{"", false},
		{"/home/u/evillink", false}, // symlink resolves to /etc → escapes
		{"/home/u/evillink/sub", false},
	}
	for _, tt := range tests {
		if got := d.saveRootAllowed(tt.path); got != tt.want {
			t.Errorf("saveRootAllowed(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestHandleCommand_TestPath_OutsideAllowlistRejected(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{"/etc": {"passwd", "shadow"}}}
	d := New(d2rConfig(), fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.allowedSaveRoots = []string{"/home/u"} // /etc is outside

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_TestPath{TestPath: &pb.TestPath{
		GameId: "d2r",
		Path:   "/etc",
	}}})
	d.handleCommand(context.Background(), cmd)

	msg := ws.sentProto("testPathResult", 0)
	if msg == nil {
		t.Fatal("missing testPathResult")
	}
	result := msg.GetTestPathResult()
	if result.Valid {
		t.Error("valid = true, want false for a path outside the allowlist")
	}
	if result.FilesFound != 0 || len(result.FileNames) != 0 {
		t.Errorf("leaked %d filenames from a disallowed path: %v", result.FilesFound, result.FileNames)
	}
}

func TestHandleConfigUpdate_SavePathOutsideAllowlistRefused(t *testing.T) {
	ws := newFakeWSClient()
	fsys := &fakeFS{dirs: map[string][]string{"/etc": {}, "/home/u/saves": {}}}
	d := New(d2rConfig(), fsys, newFakeWatcher(), &fakeRunner{}, ws, &fakePluginManager{}, nil, testLogger())
	d.allowedSaveRoots = []string{"/home/u"}

	cmd, _ := proto.Marshal(&pb.Message{Payload: &pb.Message_ConfigUpdate{ConfigUpdate: &pb.ConfigUpdate{
		Games: map[string]*pb.GameConfig{
			"d2r": {SavePath: "/etc", FileExtensions: []string{".d2s"}, Enabled: true},
		},
	}}})
	d.handleCommand(context.Background(), cmd)

	d.mu.RLock()
	stored, ok := d.cfg.Games["d2r"]
	d.mu.RUnlock()
	if ok && stored.SavePath == "/etc" {
		t.Errorf("disallowed SavePath /etc was stored as game config: %+v", stored)
	}
}

// --- Directory-unit tests ---

const palworldSaveDir = "/saves/palworld/SaveGames/76561198000000001/SAVEID1"

func palworldMembers() []string {
	return []string{"Level.sav", "LevelMeta.sav", "Players/*.sav"}
}

// palworldFS builds a fakeFS for a single Palworld-style directory-unit
// save: a top-level Level.sav/LevelMeta.sav pair, a nested Players/ member,
// and an excluded backup/ subdirectory whose contents must never appear in
// a member snapshot or tar archive.
func palworldFS() *fakeFS {
	return &fakeFS{
		dirs: map[string][]string{
			"/saves/palworld/SaveGames":                   {"76561198000000001"},
			"/saves/palworld/SaveGames/76561198000000001": {"SAVEID1"},
			palworldSaveDir:                               {"Level.sav", "LevelMeta.sav", "Players", "backup"},
			palworldSaveDir + "/Players":                  {"player1.sav"},
			palworldSaveDir + "/backup":                   {"Level.sav.bak"},
		},
		files: map[string][]byte{
			palworldSaveDir + "/Level.sav":            []byte("level data"),
			palworldSaveDir + "/LevelMeta.sav":        []byte("meta data"),
			palworldSaveDir + "/Players/player1.sav":  []byte("player data"),
			palworldSaveDir + "/backup/Level.sav.bak": []byte("stale backup data"),
		},
	}
}

func palworldConfig() Config {
	return Config{
		SourceID: "steam-deck",
		Version:  "0.1.0",
		Games: map[string]GameConfig{
			"palworld": {
				SavePath:    "/saves/palworld/SaveGames/*/*",
				ExcludeDirs: []string{"backup"},
				Enabled:     true,
			},
		},
	}
}

func palworldPluginManager() *fakePluginManager {
	return &fakePluginManager{
		manifests: map[string]pluginmgr.PluginInfo{
			"palworld": {
				GameID:       "palworld",
				Name:         "Palworld",
				Unit:         "directory",
				Members:      palworldMembers(),
				DefaultPaths: map[string]string{runtime.GOOS: "/saves/palworld/SaveGames/*/*"},
			},
		},
	}
}

func readTarEntries(t *testing.T, tarBytes []byte) (names []string, contents map[string]string) {
	t.Helper()
	contents = map[string]string{}
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names = append(names, hdr.Name)
		data, readErr := io.ReadAll(tr)
		if readErr != nil {
			t.Fatalf("tar member read: %v", readErr)
		}
		contents[hdr.Name] = string(data)
	}
	return names, contents
}

// TestReadThenTarDirectoryUnitMembers_MembersRelativeSortedExcluded covers
// the tar dispatch success criteria directly, through the P3-split pipeline
// (directoryUnitMembers walk -> readDirectoryUnitMembers hash-only pass ->
// tarFromDirUnitMembers) that replaced the old monolithic
// buildDirectoryUnitTar: member paths relative to the save root,
// deterministic sorted order, correct contents, the excluded backup/
// subdirectory omitted entirely, and one hash per member.
func TestReadThenTarDirectoryUnitMembers_MembersRelativeSortedExcluded(t *testing.T) {
	d := New(
		Config{}, palworldFS(), newFakeWatcher(), &fakeRunner{},
		newFakeWSClient(), &fakePluginManager{}, nil, testLogger(),
	)

	relPaths := d.directoryUnitMembers(palworldSaveDir, palworldMembers(), []string{"backup"})
	want := []string{"Level.sav", "LevelMeta.sav", "Players/player1.sav"}
	if !slices.Equal(relPaths, want) {
		t.Fatalf("directoryUnitMembers = %v, want %v (sorted, relative to save root)", relPaths, want)
	}

	members, err := d.readDirectoryUnitMembers(context.Background(), palworldSaveDir, relPaths)
	if err != nil {
		t.Fatalf("readDirectoryUnitMembers: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("members = %d entries, want 3", len(members))
	}

	tarBytes, err := tarFromDirUnitMembers(members)
	if err != nil {
		t.Fatalf("tarFromDirUnitMembers: %v", err)
	}

	names, contents := readTarEntries(t, tarBytes)
	if !slices.Equal(names, want) {
		t.Fatalf("tar member names = %v, want %v (sorted, relative to save root)", names, want)
	}
	if contents["Level.sav"] != "level data" || contents["LevelMeta.sav"] != "meta data" ||
		contents["Players/player1.sav"] != "player data" {
		t.Errorf("tar member contents = %+v", contents)
	}
	for _, n := range names {
		if strings.Contains(n, "backup") {
			t.Errorf("excluded backup/ subdirectory leaked into tar: %s", n)
		}
	}
}

// TestReadDirectoryUnitMembers_MemberOverSizeCapSkipped proves S3: a single
// member whose content exceeds dirUnitMemberSizeCap is skipped (logged
// warning) rather than aborting the whole read, and a normal-sized sibling
// member still comes through.
func TestReadDirectoryUnitMembers_MemberOverSizeCapSkipped(t *testing.T) {
	logger, records := newRecordingLogger()
	oversized := make([]byte, dirUnitMemberSizeCap+1)
	fsys := &fakeFS{
		dirs: map[string][]string{"/saves/unit": {"Big.sav", "Small.sav"}},
		files: map[string][]byte{
			"/saves/unit/Big.sav":   oversized,
			"/saves/unit/Small.sav": []byte("small"),
		},
	}
	d := New(Config{}, fsys, newFakeWatcher(), &fakeRunner{}, newFakeWSClient(), &fakePluginManager{}, nil, logger)

	members, err := d.readDirectoryUnitMembers(context.Background(), "/saves/unit", []string{"Big.sav", "Small.sav"})
	if err != nil {
		t.Fatalf("readDirectoryUnitMembers: %v", err)
	}
	if len(members) != 1 || members[0].rel != "Small.sav" {
		t.Fatalf("members = %+v, want only Small.sav (Big.sav must be skipped for exceeding the size cap)", members)
	}

	var warned bool
	for _, r := range records() {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "size cap") {
			warned = true
		}
	}
	if !warned {
		t.Error("expected a logged warning for the over-cap member")
	}
}

// TestReadDirectoryUnitMembers_TotalOverCapAborts proves S3: once the
// running total of member sizes exceeds dirUnitTotalSizeCap, the whole read
// aborts with an error — even though A.sav and B.sav are each individually
// under the per-member cap (exactly at it, in fact) and would otherwise be
// accepted.
func TestReadDirectoryUnitMembers_TotalOverCapAborts(t *testing.T) {
	atCap := make([]byte, dirUnitMemberSizeCap) // exactly at the per-member cap: allowed individually
	fsys := &fakeFS{
		dirs: map[string][]string{"/saves/unit": {"A.sav", "B.sav", "C.sav"}},
		files: map[string][]byte{
			"/saves/unit/A.sav": atCap,
			"/saves/unit/B.sav": atCap,
			"/saves/unit/C.sav": []byte("x"), // tips the running total past dirUnitTotalSizeCap
		},
	}
	d := New(
		Config{},
		fsys,
		newFakeWatcher(),
		&fakeRunner{},
		newFakeWSClient(),
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	_, err := d.readDirectoryUnitMembers(context.Background(), "/saves/unit", []string{"A.sav", "B.sav", "C.sav"})
	if err == nil {
		t.Fatal("expected error once the running total exceeds the directory-unit total size cap")
	}
}

// TestReadDirectoryUnitMembers_UnreadableMemberSkippedWithWarning proves S4:
// a single member that fails to read (e.g. a live race against the game
// process still writing it) is skipped with a logged warning instead of
// aborting the whole snapshot, and the archive is still built from whatever
// did read successfully.
func TestReadDirectoryUnitMembers_UnreadableMemberSkippedWithWarning(t *testing.T) {
	logger, records := newRecordingLogger()
	fsys := &fakeFS{
		dirs: map[string][]string{"/saves/unit": {"Good.sav", "Missing.sav"}},
		files: map[string][]byte{
			"/saves/unit/Good.sav": []byte("good data"),
			// Missing.sav intentionally has no fixture entry, so ReadFile fails.
		},
	}
	d := New(Config{}, fsys, newFakeWatcher(), &fakeRunner{}, newFakeWSClient(), &fakePluginManager{}, nil, logger)

	members, err := d.readDirectoryUnitMembers(context.Background(), "/saves/unit", []string{"Good.sav", "Missing.sav"})
	if err != nil {
		t.Fatalf("readDirectoryUnitMembers: %v", err)
	}
	if len(members) != 1 || members[0].rel != "Good.sav" {
		t.Fatalf("members = %+v, want only Good.sav (archive still built without the unreadable member)", members)
	}

	var warned bool
	for _, r := range records() {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "unreadable") {
			warned = true
		}
	}
	if !warned {
		t.Error("expected a logged warning for the unreadable member")
	}
}

// TestReadDirectoryUnitMembers_AllUnreadableAborts proves the S4 "abort only
// if ZERO members remain" boundary: when every candidate member fails to
// read, the snapshot aborts with an error rather than silently dispatching
// an empty archive — a live race explains one bad member, never all of
// them, so this signals a real problem worth surfacing.
func TestReadDirectoryUnitMembers_AllUnreadableAborts(t *testing.T) {
	fsys := &fakeFS{dirs: map[string][]string{"/saves/unit": {"A.sav", "B.sav"}}}
	d := New(
		Config{},
		fsys,
		newFakeWatcher(),
		&fakeRunner{},
		newFakeWSClient(),
		&fakePluginManager{},
		nil,
		testLogger(),
	)

	_, err := d.readDirectoryUnitMembers(context.Background(), "/saves/unit", []string{"A.sav", "B.sav"})
	if err == nil {
		t.Fatal("expected error when every candidate member fails to read (zero members remain)")
	}
}

// TestScanGame_DirectoryUnit_SaveCountIsOnePerDirectory confirms a
// directory-unit save counts as exactly ONE save, not one per member file
// (there are 3 matching member files in the fixture, but 1 save directory).
func TestScanGame_DirectoryUnit_SaveCountIsOnePerDirectory(t *testing.T) {
	ws := newFakeWSClient()
	watcher := newFakeWatcher()
	runner := &fakeRunner{
		results: map[string]*GameState{"palworld": newD2RState()},
	}
	cfg := palworldConfig()
	pm := palworldPluginManager()

	d := New(cfg, palworldFS(), watcher, runner, ws, pm, nil, testLogger())
	d.scanGame(context.Background(), "palworld", cfg.Games["palworld"], false)

	detected := ws.sentProto("gameDetected", 0)
	if detected == nil {
		t.Fatal("missing gameDetected")
	}
	if saveCount := detected.GetGameDetected().SaveCount; saveCount != 1 {
		t.Errorf("SaveCount = %d, want 1 (one directory save, not 3 member files)", saveCount)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if runner.calls[0].FileName != "SAVEID1" {
		t.Errorf("fileName = %q, want the save directory's own base name %q", runner.calls[0].FileName, "SAVEID1")
	}
	names, _ := readTarEntries(t, runner.calls[0].SaveBytes)
	if len(names) != 3 {
		t.Errorf("dispatched tar has %d members, want 3", len(names))
	}

	if len(watcher.addedDirUnits) != 1 {
		t.Fatalf("AddDirectoryUnit called %d times, want 1", len(watcher.addedDirUnits))
	}
	if watcher.addedDirUnits[0].Root != palworldSaveDir {
		t.Errorf("AddDirectoryUnit root = %q, want %q", watcher.addedDirUnits[0].Root, palworldSaveDir)
	}
	if !slices.Equal(watcher.addedDirUnits[0].ExcludeDirs, []string{"backup"}) {
		t.Errorf("AddDirectoryUnit excludeDirs = %v, want [backup]", watcher.addedDirUnits[0].ExcludeDirs)
	}

	d.mu.RLock()
	gameID, watched := d.watchedDirs[palworldSaveDir]
	d.mu.RUnlock()
	if !watched || gameID != "palworld" {
		t.Errorf("watchedDirs[%q] = (%q, %v), want (palworld, true)", palworldSaveDir, gameID, watched)
	}
}

// TestHandleFileEvent_DirectoryUnit_RootRoutesAndDispatches confirms a
// coalesced root FileEvent (as the watcher emits for directory units — see
// watcher.AddDirectoryUnit) is recognized directly, without requiring
// filepath.Dir(ev.Path) to match a watched directory — the nested-event
// routing fix. A write nested under Players/ is represented by the watcher's
// already-coalesced root event; this test exercises the daemon's handling of
// that root event.
func TestHandleFileEvent_DirectoryUnit_RootRoutesAndDispatches(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"palworld": newD2RState()}}
	cfg := palworldConfig()
	pm := palworldPluginManager()

	d := New(cfg, palworldFS(), newFakeWatcher(), runner, ws, pm, nil, testLogger())
	d.watchedDirs[palworldSaveDir] = "palworld"

	d.handleFileEvent(context.Background(), FileEvent{Path: palworldSaveDir, Op: FileModify})

	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	if runner.calls[0].GameID != "palworld" {
		t.Errorf("gameID = %q, want palworld", runner.calls[0].GameID)
	}
	types := ws.sentEventTypes()
	if !slices.Contains(types, "pushSave") {
		t.Error("missing pushSave event")
	}
}

// TestHandleFileEvent_DirectoryUnit_AggregateDedup_UnchangedSkipsReparse
// confirms the aggregate (path,hash) dedup: firing the root event twice with
// identical on-disk content re-parses only once.
func TestHandleFileEvent_DirectoryUnit_AggregateDedup_UnchangedSkipsReparse(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"palworld": newD2RState()}}
	cfg := palworldConfig()
	pm := palworldPluginManager()

	d := New(cfg, palworldFS(), newFakeWatcher(), runner, ws, pm, nil, testLogger())
	d.watchedDirs[palworldSaveDir] = "palworld"

	d.handleFileEvent(context.Background(), FileEvent{Path: palworldSaveDir, Op: FileModify})
	d.handleFileEvent(context.Background(), FileEvent{Path: palworldSaveDir, Op: FileModify})

	if len(runner.calls) != 1 {
		t.Errorf("runner called %d times, want 1 (unchanged member set must not re-parse)", len(runner.calls))
	}
}

// TestHandleFileEvent_DirectoryUnit_AggregateDedup_ChangedMemberReparses
// confirms a genuine content change to one member re-triggers the parse.
func TestHandleFileEvent_DirectoryUnit_AggregateDedup_ChangedMemberReparses(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"palworld": newD2RState()}}
	cfg := palworldConfig()
	pm := palworldPluginManager()
	fsys := palworldFS()

	d := New(cfg, fsys, newFakeWatcher(), runner, ws, pm, nil, testLogger())
	d.watchedDirs[palworldSaveDir] = "palworld"

	d.handleFileEvent(context.Background(), FileEvent{Path: palworldSaveDir, Op: FileModify})
	fsys.files[palworldSaveDir+"/Level.sav"] = []byte("level data v2")
	d.handleFileEvent(context.Background(), FileEvent{Path: palworldSaveDir, Op: FileModify})

	if len(runner.calls) != 2 {
		t.Errorf("runner called %d times, want 2 (changed member must re-parse)", len(runner.calls))
	}
}

// TestDiscoverGames_DirectoryUnit_CountsDirectoriesNotMembers confirms
// discovery counts qualifying save directories, not their member files —
// consistent with the scanGame SaveCount semantics.
func TestDiscoverGames_DirectoryUnit_CountsDirectoriesNotMembers(t *testing.T) {
	ws := newFakeWSClient()
	fsys := palworldFS()
	// Add a second save slot directory under the same Steam ID.
	fsys.dirs["/saves/palworld/SaveGames/76561198000000001"] = []string{"SAVEID1", "SAVEID2"}
	fsys.dirs["/saves/palworld/SaveGames/76561198000000001/SAVEID2"] = []string{"Level.sav"}
	fsys.files["/saves/palworld/SaveGames/76561198000000001/SAVEID2/Level.sav"] = []byte("level2")

	pm := palworldPluginManager()
	d := New(Config{Games: map[string]GameConfig{}}, fsys, newFakeWatcher(), &fakeRunner{}, ws, pm, nil, testLogger())
	d.discoverGames(context.Background())

	gd := ws.sentProto("gamesDiscovered", 0).GetGamesDiscovered()
	if len(gd.Games) != 1 {
		t.Fatalf("games count = %d, want 1", len(gd.Games))
	}
	if gd.Games[0].FileCount != 2 {
		t.Errorf("FileCount = %d, want 2 (two save directories, not member files)", gd.Games[0].FileCount)
	}
}

// TestPluginChanged_DirectoryUnit_DispatchesOneTarNoPerFileEvents proves
// pluginChanged branches directory-unit games to rescanQuietDirectoryUnit
// (mirroring scanGame) instead of unconditionally calling rescanQuiet, whose
// ReadDir+filterSaveFiles path would dispatch raw member files under file
// identities to a plugin that expects one tar archive under the directory
// identity. It also proves the stale dirUnitSnapshotHashes entry for the
// save directory is invalidated, so the forced re-parse below isn't itself
// suppressed by the aggregate dedup.
func TestPluginChanged_DirectoryUnit_DispatchesOneTarNoPerFileEvents(t *testing.T) {
	ws := newFakeWSClient()
	runner := &fakeRunner{results: map[string]*GameState{"palworld": newD2RState()}}
	cfg := palworldConfig()
	pm := palworldPluginManager()

	d := New(cfg, palworldFS(), newFakeWatcher(), runner, ws, pm, nil, testLogger())
	// Initial scan watches the save directory and dispatches the initial
	// tar, seeding dirUnitSnapshotHashes with the current (unchanged) content.
	d.scanGame(context.Background(), "palworld", cfg.Games["palworld"], false)
	if len(runner.calls) != 1 {
		t.Fatalf("setup: runner called %d times, want 1", len(runner.calls))
	}

	d.pluginChanged(context.Background(), "palworld", cfg.Games["palworld"])

	if len(runner.calls) != 2 {
		t.Fatalf(
			"runner called %d times after pluginChanged, want 2 (exactly one forced re-dispatch)",
			len(runner.calls),
		)
	}
	second := runner.calls[1]
	if second.GameID != "palworld" {
		t.Errorf("gameID = %q, want palworld", second.GameID)
	}
	if second.FileName != "SAVEID1" {
		t.Errorf(
			"fileName = %q, want the save directory's own base name %q (directory identity, not a raw member)",
			second.FileName, "SAVEID1",
		)
	}
	names, _ := readTarEntries(t, second.SaveBytes)
	if len(names) != 3 {
		t.Errorf("re-dispatched tar has %d members, want 3", len(names))
	}
	for _, call := range runner.calls {
		if call.FileName == "Level.sav" || call.FileName == "LevelMeta.sav" {
			t.Errorf("per-file raw dispatch leaked through: fileName=%q", call.FileName)
		}
	}
}

// TestDirectoryUnitMembers_RecursesRealDirsSkipsSymlinks proves the walk
// recurses using the ReadDir entry's own IsDir (never a followed stat, which
// is what let a symlinked directory tree get archived and a symlink cycle
// recurse unbounded): "linked" is a symlink to a real directory — Stat
// follows it and would (with the old, buggy Stat-based walk) report it as a
// dir, but the ReadDir entry for it correctly reports non-dir without
// following, exactly like a real os.DirEntry. It also proves the S4 fix:
// "linked" itself is entirely excluded from the member set (a symlink is
// never archived, whether it targets a file or a directory — otherwise its
// target's content, potentially outside the save directory, would end up in
// the tar). A real nested subdirectory (realsub) must still recurse and have
// its member matched.
func TestDirectoryUnitMembers_RecursesRealDirsSkipsSymlinks(t *testing.T) {
	fsys := &fakeFS{
		dirs: map[string][]string{
			"/root":             {"realsub", "linked"},
			"/root/realsub":     {"a.sav"},
			"/elsewhere/target": {},
		},
		files: map[string][]byte{
			"/root/realsub/a.sav": []byte("data"),
		},
		symlinks: map[string]string{
			"/root/linked": "/elsewhere/target",
		},
	}
	d := &Daemon{fs: fsys}

	got := d.directoryUnitMembers("/root", nil, nil)

	if !slices.Contains(got, "realsub/a.sav") {
		t.Errorf("members = %v, want to contain realsub/a.sav (real subdirectory must recurse)", got)
	}
	if slices.Contains(got, "linked") {
		t.Errorf(
			"members = %v, must not contain \"linked\" (a symlink entry must never be archived as a member)",
			got,
		)
	}
	if slices.Contains(fsys.readDirCalls, "/root/linked") {
		t.Errorf(
			"readDirCalls = %v, must never contain /root/linked (a symlinked directory must never be recursed into)",
			fsys.readDirCalls,
		)
	}
}

// TestDirectoryUnitMembers_SkipsFileSymlink proves the S4 symlink skip also
// covers a symlink whose target is a regular file, not just a directory: a
// symlinked file that would otherwise match every member pattern (nil
// members) must still be excluded from the candidate set.
func TestDirectoryUnitMembers_SkipsFileSymlink(t *testing.T) {
	fsys := &fakeFS{
		dirs: map[string][]string{
			"/root": {"Level.sav", "Level.sav.link"},
		},
		files: map[string][]byte{
			"/root/Level.sav": []byte("real data"),
		},
		symlinks: map[string]string{
			"/root/Level.sav.link": "/root/Level.sav",
		},
	}
	d := &Daemon{fs: fsys}

	got := d.directoryUnitMembers("/root", nil, nil)

	if !slices.Contains(got, "Level.sav") {
		t.Errorf("members = %v, want to contain the real file Level.sav", got)
	}
	if slices.Contains(got, "Level.sav.link") {
		t.Errorf("members = %v, must not contain the symlinked file Level.sav.link", got)
	}
}

// TestMatchesMember_SlashSeparatorExact proves member matching is
// slash-exact regardless of the host OS path separator: members are
// authored with '/' (e.g. "Players/*.sav"), and matchesMember must match a
// slash-separated rel using path.Match semantics, not filepath.Match (which
// on Windows would compare against a backslash-separated path and never
// match a nested member pattern).
func TestMatchesMember_SlashSeparatorExact(t *testing.T) {
	if !matchesMember("Players/player1.sav", []string{"Players/*.sav"}) {
		t.Error("matchesMember failed to match a nested member against its slash-separated pattern")
	}
	if matchesMember("Players\\player1.sav", []string{"Players/*.sav"}) {
		t.Error("matchesMember matched a backslash-separated path against a slash pattern (separator not exact)")
	}
}

// TestAggregateHash_NoDelimiterCollision proves the length-prefixed
// serialization cannot conflate two distinct member snapshots that would
// collide under a naive "name:hexhash\n" delimiter join: a crafted member
// name containing another entry's exact "name:hexhash\n" bytes produces a
// provably different aggregate hash from the two-entry set it mimics.
func TestAggregateHash_NoDelimiterCollision(t *testing.T) {
	h1 := sha256.Sum256([]byte("content one"))
	h2 := sha256.Sum256([]byte("content two"))

	setA := map[string][32]byte{"bar": h2, "foo": h1}
	craftedName := fmt.Sprintf("bar:%x\nfoo", h2)
	setB := map[string][32]byte{craftedName: h1}

	if aggregateHash(setA) == aggregateHash(setB) {
		t.Fatal("aggregateHash collided between two distinct member snapshots (delimiter-ambiguous serialization)")
	}
}
