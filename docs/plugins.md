# WASM Plugin System

Savecraft has two kinds of plugins, both living in `plugins/{game_id}/`:

| | Daemon Plugins (WASM) | API Adapters (TypeScript) |
|---|---|---|
| **Runtime** | wazero in daemon | Cloudflare Worker |
| **Trigger** | Filesystem event (save file changed) | OAuth callback, MCP refresh, scheduled refresh |
| **Input** | Raw file bytes on stdin | Game API responses (HTTP) |
| **Output** | GameState via ndjson stdout | GameState via TypeScript return |
| **Trust model** | Sandboxed WASM, Ed25519 signed, community code | Reviewed first-party TypeScript, no sandbox needed |
| **Credentials** | None (no network access) | OAuth tokens stored in D1 |
| **State pipeline** | Daemon → WebSocket PushSave → SourceHub DO | Worker → D1 saves + SourceHub `/set-game-status` HTTP |
| **Source kind** | `daemon` | `adapter` |
| **Example** | D2R (`.d2s` file parser) | WoW (Battle.net + Raider.io APIs) |

Both produce the same `GameState` shape (`identity`, `summary`, `sections`). Everything downstream — D1 storage, FTS indexing, MCP tools, notes, search — works identically regardless of source kind.

This document covers daemon WASM plugins. For API adapters, see [docs/adapters.md](adapters.md).

## Why WASM

- **Cross-platform:** One .wasm binary works on Windows x86, Linux x86, Linux ARM (Steam Deck). No per-platform compilation for plugins.
- **Sandboxed:** Plugins cannot access filesystem, network, or environment. They process bytes the daemon feeds them via stdin and emit JSON to stdout. Structurally impossible to exfiltrate data.
- **Community-friendly:** Contributors write parsers in Go (or Rust/Zig), compile to WASM. Same toolchain as the daemon for Go plugins.
- **Language-agnostic build:** Each plugin provides a `Justfile` with a `just build` target that produces a `.wasm` file in the plugin directory. The top-level `just build-plugin <name>` delegates to the plugin's own build. The daemon doesn't care what language the plugin is written in — only that it speaks WASI Preview 1 with the ndjson contract.

## Runtime: wazero

Pure-Go WASM runtime. No CGO, no libc, no external dependencies. Supports WASI Preview 1 (the stable, widely-implemented version). wazero compiles WASM to native machine code at load time for near-native performance.

WASI Preview 2 (Component Model) is not used — wazero doesn't support it yet, and Preview 1 is sufficient for the stdin/stdout contract.

## Plugin Contract: ndjson on stdout via WASI

Plugins are compiled as WASI executables. The daemon feeds raw save file bytes on stdin. The plugin writes newline-delimited JSON (ndjson) to stdout. No manual memory management, no malloc/free exports, no pointer arithmetic.

**Input:** Raw save file bytes on stdin.

**Output:** Newline-delimited JSON on stdout. Every line is a JSON object with a `type` field:

- `"status"` — Progress update. Optional. Plugin authors emit these for long-running or multi-step parses. The daemon forwards them to the UI via WebSocket.
- `"result"` — Final GameState output. **Required on exit code 0.** Must be the last line.
- `"error"` — Structured error. **Required on exit code 1.** Must be the last line.

**stderr** is for unstructured debug logging. The daemon captures it for diagnostics but does not parse it.

**Status line:**
```json
{"type": "status", "message": "Found 3 save files in directory"}
{"type": "status", "message": "Decoding inventory (247 items)"}
```

**Result line (success, exit code 0):**
```json
{"type": "result", "identity": {...}, "summary": "...", "sections": {...}}
```

**Error line (failure, exit code 1):**
```json
{"type": "error", "error_type": "corrupt_file", "message": "Human-readable description", "byte_offset": 1234}
```

Valid `error_type` values: `unsupported_version`, `corrupt_file`, `parse_error`.

### Section Data Contract

Each section in the `sections` map has a `description` (string) and `data` field. **Section data must be a JSON object.** It must not be an array, string, number, boolean, or null.

If a section naturally contains a list (e.g. equipped items, skill allocations), wrap the array in an object with a descriptive key:

```json
// CORRECT — object with descriptive key
"equipment": {
  "description": "Equipped items",
  "data": {"equipment": [{"slot": "head", "name": "Shako"}, ...]}
}

// CORRECT — object with multiple keys
"inventory": {
  "description": "Inventory, stash, and cube items",
  "data": {"inventory": [...], "stash": [...], "cube": [...]}
}

// WRONG — bare array
"equipment": {
  "description": "Equipped items",
  "data": [{"slot": "head", "name": "Shako"}, ...]
}

// WRONG — scalar value
"playtime": {
  "description": "Total play time",
  "data": 3600
}
```

**Enforcement:** The daemon validates that each section's data is a JSON object before sending it to the server. Sections with non-object data are skipped with an error log — they will not reach storage or MCP. The server performs the same validation on receipt.

**Plugin Go source example (D2R):**

```go
// plugins/d2r/main.go
package main

import (
    "encoding/json"
    "io"
    "os"
)

var enc = json.NewEncoder(os.Stdout)

func main() {
    data, err := io.ReadAll(os.Stdin)
    if err != nil {
        writeError("parse_error", "failed to read stdin: "+err.Error())
        os.Exit(1)
    }

    enc.Encode(map[string]string{"type": "status", "message": "Read " + fmt.Sprintf("%d", len(data)) + " bytes"})

    state, err := ParseD2S(data)
    if err != nil {
        writeError("corrupt_file", err.Error())
        os.Exit(1)
    }

    enc.Encode(map[string]any{
        "type":     "result",
        "identity": state.Identity,
        "summary":  state.Summary,
        "sections": state.Sections,
    })
}

func writeError(errType, message string) {
    enc.Encode(map[string]string{
        "type":       "error",
        "error_type": errType,
        "message":    message,
    })
}
```

Simple plugins that don't need progress updates just emit a single result line. The status lines are optional.

**Reference plugin (echo):** `plugins/echo/` is a minimal plugin that reads stdin and reflects its content back as a GameState. It validates the ndjson contract and wazero integration end-to-end without any game-specific logic. Tests use it to verify the runner, status forwarding, and error paths.

**Daemon-side execution with wazero:**

```go
// Pseudocode for plugin execution
ctx := context.Background()
r := wazero.NewRuntime(ctx)
wasi_snapshot_preview1.MustInstantiate(ctx, r)

// stdout is a pipe — daemon reads ndjson lines as they arrive
stdoutR, stdoutW := io.Pipe()
var stderr bytes.Buffer

config := wazero.NewModuleConfig().
    WithStdin(bytes.NewReader(saveFileBytes)).
    WithStdout(stdoutW).
    WithStderr(&stderr)

// Read stdout lines in a goroutine
go func() {
    scanner := bufio.NewScanner(stdoutR)
    for scanner.Scan() {
        line := scanner.Bytes()
        msg := parsePluginLine(line)
        switch msg.Type {
        case "status":
            // Forward to WebSocket as PluginStatus event
            ws.Send(PluginStatus{GameID: gameID, FileName: fileName, Message: msg.Message})
        case "result":
            // Store as the final GameState
            gameState = msg.GameState
        case "error":
            // Store as the parse error
            parseErr = msg.Error
        }
    }
}()

mod, err := r.InstantiateWithConfig(ctx, pluginWasm, config)
stdoutW.Close()
// Check exit code, use gameState or parseErr
```

**Size limit:** The daemon enforces a 2MB hard cap on the result line. If a plugin emits a result larger than 2MB, the daemon treats it as a parse error and logs a warning. Typical game state JSON is 10-500KB.

## Plugin Metadata (`plugin.toml`)

Each production plugin has a `plugin.toml` in its directory — the single source of truth for plugin metadata. Test plugins (echo, error, noop, crash) are dev-only and have no `plugin.toml`.

```toml
game_id = "d2r"
name = "Diablo II: Resurrected"
description = "Parses .d2s character save files from Reign of the Warlock (v105)"
version = "0.0.1"
channel = "beta"                          # "beta" or "stable"
coverage = "partial"                      # "partial" or "full"
file_extensions = [".d2s"]
homepage = "https://savecraft.gg/plugins/d2r"

limitations = [
  "Shared stash (.d2i) not yet supported",
  "Only Reign of the Warlock (v105) saves — classic LoD not supported",
]

[author]
name = "Josh Symonds"
github = "joshsymonds"

[default_paths]
windows = "%USERPROFILE%/Saved Games/Diablo II Resurrected"
linux = "~/.local/share/Diablo II Resurrected"
darwin = "~/Library/Application Support/Diablo II Resurrected"

# Optional: reference modules for server-side computation
[reference.modules.drop_calc]
name = "Drop Calculator"
description = "Compute drop probabilities for any item from any farmable source."

[reference.modules.drop_calc.attribution]
author = "Josh Symonds"
data_sources = [
  { name = "TreasureClassEx.txt", origin = "Diablo II game data tables" },
]
```

**Field reference:**

| Field | Required | Description |
|-------|----------|-------------|
| `game_id` | yes | Unique identifier, matches plugin directory name |
| `name` | yes | Human-readable game title |
| `description` | yes | What the plugin parses |
| `version` | yes | Semver. Bump to trigger a plugin release |
| `channel` | yes | `"beta"` or `"stable"` — daemon can filter by channel |
| `coverage` | yes | `"partial"` (known limitations) or `"full"` |
| `file_extensions` | yes | Save file extensions this plugin handles |
| `homepage` | no | URL for plugin documentation |
| `limitations` | no | Known gaps, shown in UI and MCP responses |
| `author.name` | yes | Plugin author's display name |
| `author.github` | yes | GitHub username |
| `default_paths` | yes | Per-OS default save directory (env vars and `~` expanded by daemon) |
| `reference.modules.*` | no | Reference modules for server-side computation (name, description, attribution) |

The daemon resolves environment variables and `~` in default paths at startup. If a declared path exists, it auto-configures. The user can override paths via the web settings UI; overrides are stored per-source in D1.

## Reference Modules (Server-Side WASM)

Plugins can optionally ship a second WASM binary (`reference.wasm`) for server-side computation — drop calculators, breakpoint tables, item databases. These run in the cloud, not the daemon.

### Architecture: Workers for Platforms

Each reference plugin deploys as its own Cloudflare Worker via Workers for Platforms (WfP). The main Savecraft Worker dispatches to reference Workers through a `DispatchNamespace` binding:

```
MCP client → main Worker → env.REFERENCE_PLUGINS.get("d2r-reference").fetch(request)
                          → D2R reference Worker (static WASM import + WASI shim)
                          → reference.wasm executes query via stdin/stdout
                          ← ndjson result
```

**Why WfP, not inline execution?** `WebAssembly.compile()` is blocked by workerd's V8 security policy everywhere — production, dev, and tests. WfP solves this by pre-compiling WASM at deploy time via static `import` statements. Each reference Worker is a pure computation sandbox with zero bindings (no KV, no R2, no D1).

### Reference Worker Structure

The shared reference Worker (`reference/`) is a game-agnostic WASI shim that executes any plugin's `reference.wasm`:

```
reference/
├── src/
│   ├── index.ts          # Static WASM import + WASI shim execution
│   ├── wasi-shim.ts      # Minimal WASI Preview 1 adapter (~235 LOC)
│   └── wasm.d.ts         # Type declaration for .wasm imports
├── test/
│   └── reference.test.ts # Standalone tests via vitest-pool-workers
├── wrangler.toml         # Zero bindings — pure computation sandbox
├── vitest.config.ts
└── package.json
```

The Worker accepts POST requests, passes the body as stdin to the WASM module, and returns stdout as the response. The WASI shim provides only `fd_read` (stdin) and `fd_write` (stdout/stderr) — no filesystem, no network, no environment access. CI copies each game's `reference.wasm` into this directory and deploys as `{game_id}-reference` to the WfP dispatch namespace.

### Reference Contract

Same ndjson contract as parsers, but with JSON query input instead of binary save data:

- **Input:** JSON query string on stdin
- **Output:** ndjson on stdout (`{"type": "result", ...}` or `{"type": "error", ...}`)
- **Schema discovery:** Empty query `{}` returns the module's parameter schema

### Dispatch Namespace Setup

The main Worker has a `REFERENCE_PLUGINS` dispatch namespace binding configured in `wrangler.toml`. Reference Workers are deployed to the namespace with names following the pattern `{game_id}-reference` (e.g., `d2r-reference`).

## Plugin Manifest (`manifest.json`)

`manifest.json` is a **generated** artifact — never hand-edited. The `cmd/plugin-manifest/` Go tool reads `plugin.toml`, computes the sha256 of the built `.wasm` binaries, and writes `manifest.json` with all fields plus `sha256` and `url`. This manifest is uploaded to R2 alongside the signed WASM binaries.

```
just plugin-manifest d2r          # generate plugins/d2r/manifest.json
just build-plugin d2r             # build the .wasm first
```

The manifest endpoint `GET /api/v1/plugins/manifest` returns all fields from R2 — version, sha256, name, description, channel, coverage, file_extensions, default_paths, limitations, author, homepage — plus a resolved download URL. The worker passes through whatever the manifest contains with no filtering.

## Plugin Distribution

Plugins are hosted alongside their manifests in R2:

```
plugins/{game_id}/manifest.json
plugins/{game_id}/parser.wasm
plugins/{game_id}/parser.wasm.sig
plugins/{game_id}/reference.wasm        # optional, if plugin has reference modules
plugins/{game_id}/reference.wasm.sig
```

**Dual targets:** Plugins can optionally ship two WASM binaries from shared source. `parser.wasm` runs in the daemon (save file parsing). `reference.wasm` runs in the Worker (server-side reference data computation like drop rates). Both use the same ndjson stdin/stdout contract and Ed25519 signing.

**Polling:** Daemon checks `GET /api/v1/plugins/manifest` on startup and every 24 hours. Response is a JSON object mapping game IDs to plugin metadata (version, sha256, url, plus all `plugin.toml` fields). If the plugin has a reference module, the manifest includes a `reference` field with its own sha256, url, and module list. Daemon compares local versions, downloads updates as needed.

**Signing:** Every `.wasm` binary is signed with an Ed25519 private key held by Savecraft (`SIGNING_PRIVATE_KEY` in GitHub Actions secrets, base64-encoded raw 32-byte key). A `.sig` file ships alongside each `.wasm`. The daemon has the public key baked in (`internal/signing/signing_key.pub`) and verifies signatures before loading. Unsigned or tampered modules are refused.

**Trust model:** Community contributors submit PRs with parser source code to the `plugins/` directory in the monorepo. Maintainer reviews source code and merges. Release pipeline builds the WASM, signs the binary, and uploads to R2. Same model as Linux package signing.
