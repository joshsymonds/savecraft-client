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

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

const maxResultSize = 2 * 1024 * 1024 // 2MB

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

// WazeroRunner runs WASM plugins using the wazero runtime.
// It satisfies the daemon.Runner interface.
type WazeroRunner struct {
	runtime  wazero.Runtime
	modules  map[string]wazero.CompiledModule
	mu       sync.RWMutex
	counter  atomic.Uint64
	verifier func(wasmBytes, sigBytes []byte) error
}

// NewWazeroRunner creates a new WazeroRunner backed by a wazero runtime with
// WASI snapshot preview1 support.
func NewWazeroRunner(ctx context.Context, opts ...Option) (*WazeroRunner, error) {
	rt := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		if closeErr := rt.Close(ctx); closeErr != nil {
			return nil, errors.Join(
				fmt.Errorf("instantiate wasi: %w", err),
				fmt.Errorf("close runtime: %w", closeErr),
			)
		}
		return nil, fmt.Errorf("instantiate wasi: %w", err)
	}
	wr := &WazeroRunner{
		runtime: rt,
		modules: make(map[string]wazero.CompiledModule),
	}
	for _, opt := range opts {
		opt(wr)
	}
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
		WithArgs(gameID) // argv[0]

	var (
		result   *daemon.GameState
		parseErr error
	)

	var wg sync.WaitGroup
	wg.Go(func() {
		result, parseErr = wr.parsePluginOutput(stdoutR, onStatus)
	})

	mod, instantiateErr := wr.runtime.InstantiateModule(ctx, compiled, config)
	if err := stdoutW.Close(); err != nil {
		return nil, fmt.Errorf("close stdout pipe: %w", err)
	}
	wg.Wait()

	if mod != nil {
		if err := mod.Close(ctx); err != nil {
			return nil, fmt.Errorf("close module: %w", err)
		}
	}

	// Structured ndjson error always takes priority.
	if parseErr != nil {
		return nil, parseErr
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
					CharacterName: msg.Identity.CharacterName,
					GameID:        msg.Identity.GameID,
					Extra:         msg.Identity.Extra,
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
	CharacterName string         `json:"characterName,omitempty"`
	GameID        string         `json:"gameId"`
	Extra         map[string]any `json:"extra,omitempty"`
}
