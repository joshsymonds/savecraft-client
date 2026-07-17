// Package runner provides WASM plugin execution using the wazero runtime.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/joshsymonds/savecraft-client/internal/daemon"
	"github.com/joshsymonds/savecraft-client/internal/signing"
)

const maxResultSize = 2 * 1024 * 1024 // 2MB

// defaultMaxMemoryPages caps a plugin's Wasm linear memory. 1 page = 64 KiB,
// so 16384 pages = 1 GiB. This is far below the wasm32 ceiling of 65536 pages
// (4 GiB, above which wazero panics) yet generous enough for the largest real
// parser workloads (e.g. multi-hundred-MiB grand-strategy saves). A plugin
// that grows past this traps instead of OOM-killing the long-lived daemon.
const defaultMaxMemoryPages uint32 = 16384

// defaultParseTimeout bounds a single plugin execution. Combined with
// WithCloseOnContextDone, a wedged or infinite-looping plugin is unwound
// rather than pinning a daemon goroutine (and OS thread) forever. Sized with
// margin over legitimate large-save parses.
const defaultParseTimeout = 60 * time.Second

// Option configures a WazeroRunner.
type Option func(*WazeroRunner)

// WithVerifier enables Ed25519 signature verification on plugin load.
// When set, LoadPlugin requires a valid signature for the WASM bytes.
func WithVerifier(publicKey ed25519.PublicKey) Option {
	return func(wr *WazeroRunner) {
		wr.verifier = func(wasmBytes, sigBytes []byte) error {
			return signing.Verify(publicKey, wasmBytes, sigBytes)
		}
	}
}

// WithMemoryLimitPages overrides the per-plugin Wasm memory cap (in 64 KiB
// pages). Production uses defaultMaxMemoryPages; tests use a small value to
// exercise the limit deterministically. It can only tighten or set the cap,
// never disable it — there is always a finite limit.
func WithMemoryLimitPages(pages uint32) Option {
	return func(wr *WazeroRunner) {
		wr.maxMemoryPages = pages
	}
}

// WithParseTimeout overrides the per-execution timeout. Production uses
// defaultParseTimeout; tests use a short value to exercise the timeout path.
func WithParseTimeout(d time.Duration) Option {
	return func(wr *WazeroRunner) {
		wr.parseTimeout = d
	}
}

// WazeroRunner runs WASM plugins using the wazero runtime.
// It satisfies the daemon.Runner interface.
type WazeroRunner struct {
	runtime        wazero.Runtime
	modules        map[string]wazero.CompiledModule
	mu             sync.RWMutex
	counter        atomic.Uint64
	verifier       func(wasmBytes, sigBytes []byte) error
	maxMemoryPages uint32
	parseTimeout   time.Duration
}

// NewWazeroRunner creates a new WazeroRunner backed by a wazero runtime with
// WASI snapshot preview1 support.
func NewWazeroRunner(ctx context.Context, opts ...Option) (*WazeroRunner, error) {
	wr := &WazeroRunner{
		modules:        make(map[string]wazero.CompiledModule),
		maxMemoryPages: defaultMaxMemoryPages,
		parseTimeout:   defaultParseTimeout,
	}
	// Options must be applied before the runtime is created: the memory cap is
	// fixed at runtime-config time.
	for _, opt := range opts {
		opt(wr)
	}

	cfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(wr.maxMemoryPages).
		// Terminate guest execution when the call context is canceled or
		// times out, so an untrusted plugin cannot wedge a goroutine/OS
		// thread forever.
		WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		if closeErr := rt.Close(ctx); closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("instantiate wasi: %w", err),
				fmt.Errorf("close runtime: %w", closeErr),
			)
		}
		return nil, fmt.Errorf("instantiate wasi: %w", err)
	}
	wr.runtime = rt
	return wr, nil
}

// LoadPlugin compiles a WASM binary and registers it for the given game ID.
// When a verifier is configured, sigBytes must contain a valid Ed25519 signature.
func (wr *WazeroRunner) LoadPlugin(ctx context.Context, gameID string, wasmBytes, sigBytes []byte) error {
	if wr.verifier != nil {
		if err := wr.verifier(wasmBytes, sigBytes); err != nil {
			return fmt.Errorf("verify plugin %s: %w", gameID, err)
		}
	}
	compiled, err := wr.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile plugin %s: %w", gameID, err)
	}
	wr.mu.Lock()
	wr.modules[gameID] = compiled
	wr.mu.Unlock()
	return nil
}

// Run executes the plugin for gameID, feeding saveBytes on stdin and parsing ndjson from stdout.
// Status lines are forwarded via onStatus. Returns the final GameState or an error.
func (wr *WazeroRunner) Run(
	ctx context.Context,
	gameID string,
	fileName string,
	saveBytes []byte,
	onStatus func(string),
) (*daemon.GameState, error) {
	wr.mu.RLock()
	compiled, ok := wr.modules[gameID]
	wr.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no plugin loaded for game %s", gameID)
	}

	stdoutR, stdoutW := io.Pipe()
	var stderr bytes.Buffer

	id := wr.counter.Add(1)
	config := wazero.NewModuleConfig().
		WithName(fmt.Sprintf("plugin-%s-%d", gameID, id)).
		WithStdin(bytes.NewReader(saveBytes)).
		WithStdout(stdoutW).
		WithStderr(&stderr).
		WithArgs(gameID, fileName) // argv[0]=gameID, argv[1]=fileName

	var (
		result   *daemon.GameState
		parseErr error
	)

	var wg sync.WaitGroup
	wg.Go(func() {
		result, parseErr = wr.parsePluginOutput(stdoutR, onStatus)
	})

	// Bound every execution. With WithCloseOnContextDone(true) a timeout or a
	// parent-context cancel unwinds the guest instead of pinning this
	// goroutine (and its OS thread) forever.
	runCtx, cancel := context.WithTimeout(ctx, wr.parseTimeout)
	defer cancel()

	mod, instantiateErr := wr.runtime.InstantiateModule(runCtx, compiled, config)
	if err := stdoutW.Close(); err != nil {
		return nil, fmt.Errorf("close stdout pipe: %w", err)
	}
	wg.Wait()

	if mod != nil {
		// Tear down with a context independent of runCtx: cleanup must run
		// even when the execution was canceled or timed out.
		if err := mod.Close(context.WithoutCancel(ctx)); err != nil {
			return nil, fmt.Errorf("close module: %w", err)
		}
	}

	// Structured ndjson error always takes priority.
	if parseErr != nil {
		return nil, parseErr
	}

	// A canceled/timed-out execution surfaces as a clear error rather than
	// the opaque trap that WithCloseOnContextDone raises.
	if ctxErr := runCtx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("plugin %s execution aborted: %w (stderr: %s)", gameID, ctxErr, stderr.String())
	}

	// Handle WASI exit codes.
	if instantiateErr != nil {
		var exitErr *sys.ExitError
		if errors.As(instantiateErr, &exitErr) && exitErr.ExitCode() == 0 {
			instantiateErr = nil
		}
		if instantiateErr != nil {
			return nil, fmt.Errorf("plugin execution failed: %w (stderr: %s)", instantiateErr, stderr.String())
		}
	}

	if result == nil {
		return nil, fmt.Errorf("plugin produced no result (stderr: %s)", stderr.String())
	}

	return result, nil
}

// parsePluginOutput reads ndjson lines from the plugin's stdout, forwarding
// status messages via onStatus and returning the parsed GameState or error.
func (wr *WazeroRunner) parsePluginOutput(
	stdoutR io.Reader,
	onStatus func(string),
) (*daemon.GameState, error) {
	scanner := bufio.NewScanner(stdoutR)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResultSize)

	var (
		result   *daemon.GameState
		parseErr error
	)

	for scanner.Scan() {
		line := scanner.Bytes()
		var msg pluginLine
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "status":
			if onStatus != nil {
				onStatus(msg.Message)
			}
		case "result":
			if len(line) > maxResultSize {
				parseErr = fmt.Errorf("result exceeds %d byte limit", maxResultSize)
				continue
			}
			result = &daemon.GameState{
				Identity: daemon.Identity{
					SaveName:    msg.Identity.SaveName,
					GameID:      msg.Identity.GameID,
					Extra:       msg.Identity.Extra,
					DisplayName: msg.Identity.DisplayName,
				},
				Summary:  msg.Summary,
				Sections: msg.Sections,
			}
		case "error":
			parseErr = &daemon.PluginError{
				Type:       msg.ErrorType,
				Message:    msg.Message,
				ByteOffset: msg.ByteOffset,
			}
		}
	}

	return result, parseErr
}

// Close shuts down the wazero runtime.
func (wr *WazeroRunner) Close(ctx context.Context) error {
	if err := wr.runtime.Close(ctx); err != nil {
		return fmt.Errorf("close wazero runtime: %w", err)
	}
	return nil
}

// pluginLine represents one line of ndjson from plugin stdout.
type pluginLine struct {
	Type string `json:"type"`

	// status
	Message string `json:"message,omitempty"`

	// result
	Identity pluginIdentity            `json:"identity"`
	Summary  string                    `json:"summary,omitempty"`
	Sections map[string]daemon.Section `json:"sections,omitempty"`

	// error
	ErrorType  string `json:"errorType,omitempty"`
	ByteOffset int64  `json:"byteOffset,omitempty"`
}

type pluginIdentity struct {
	SaveName    string         `json:"saveName,omitempty"`
	GameID      string         `json:"gameId"`
	Extra       map[string]any `json:"extra,omitempty"`
	DisplayName string         `json:"displayName,omitempty"`
}
