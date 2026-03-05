# Daemon

## Architecture: Interface-Driven with Fakes

The daemon orchestrator (`internal/daemon/`) defines interfaces for all external dependencies: `Watcher`, `Runner`, `PushClient`, `WSClient`, `FS`, `PluginManager`. Tests inject hand-written fakes. Real implementations live in separate packages (`internal/runner/`, `internal/watcher/`, etc.) and satisfy the interfaces implicitly.

The `Daemon.Run()` loop: register source (if first boot) → connect WebSocket → send `SourceOnline` → scan configured games → enter event loop (file events, WS commands, context cancellation). On shutdown, send `SourceOffline`.

## First-Boot Source Registration

On first boot, the daemon has no source token. It self-registers as a source by calling `POST /api/v1/source/register` (unauthenticated). The server returns a `sct_*` source token, source UUID, and a 6-digit link code (20-minute TTL).

The daemon:
1. Persists the source token and UUID to local config (`~/.config/savecraft/env`)
2. Displays the 6-digit link code to the user (CLI output, tray notification, or terminal banner)
3. Polls `GET /api/v1/source/status` periodically to check if the user has linked the source
4. If the link code expires before linking, calls `POST /api/v1/source/link-code` to get a fresh code

On subsequent boots, the daemon reads the persisted token and skips registration. The push API and WebSocket both authenticate with this source token.

## Filesystem Watching

The daemon uses fsnotify to watch save file directories.

**Debounce + hash strategy:**

1. fsnotify fires a write/rename/create event for a watched file extension.
2. Start a 500ms debounce timer. Reset on subsequent events within the window.
3. When timer expires, SHA-256 the file.
4. If hash matches last successfully parsed hash, skip (no change).
5. If hash differs, read file bytes and feed to WASM plugin.
6. If plugin returns success (exit 0), push JSON to cloud API. Store hash as last-known-good.
7. If plugin returns error (exit 1), log the error and wait for next event. The game probably hasn't finished writing yet.

This handles:
- **Temp-file-rename pattern** (most games): rename event → debounce → hash → parse. Clean.
- **In-place write pattern** (some games): multiple write events → debounce waits for writes to stop → parse.
- **Partial write corruption:** parser errors → daemon retries on next event.
- **Steam Cloud sync overwrites:** treated the same as any save change. No special handling needed.

## Save Directory Discovery

1. On startup, after sending `SourceOnline`, daemon calls `PluginManager.Manifests()` to get the full plugin manifest.
2. For each plugin, picks the `default_paths` entry for the current OS (`runtime.GOOS`).
3. Expands path templates: `~` → home directory, `%VAR%` → environment variable.
4. Checks if path exists on disk via `FS.Stat()`. If exists, counts matching files by extension via `FS.ReadDir()`.
5. Sends a single `GamesDiscovered` event listing all found games with paths and file counts.
6. User-set overrides from web UI take precedence over discovered paths when configuring.
7. The UI can re-trigger discovery via the `DiscoverGames` command.

## Plugin Loading

1. On startup and every 24 hours, daemon fetches plugin registry from `/api/v1/plugins/manifest`.
2. For each plugin: compare local version to registry version.
3. If update available: download `.wasm` and `.sig` from registry URLs.
4. Verify Ed25519 signature against baked-in public key. Refuse unsigned/tampered binaries.
5. Replace local `.wasm` file. Re-initialize wazero module for that game.
6. Plugin binaries cached locally per platform:
   - Linux: `$XDG_DATA_HOME/savecraft/plugins/` (default `~/.local/share/savecraft/plugins/`)
   - macOS: `~/Library/Application Support/Savecraft/plugins/`
   - Windows: `%LOCALAPPDATA%\Savecraft\plugins\`
   - Override: `SAVECRAFT_CACHE_DIR` env var

## WebSocket Client (Go)

Uses `nhooyr.io/websocket` for context-aware WebSocket with clean shutdown.

**Connection lifecycle:**
1. On startup, connect to `wss://api.savecraft.gg/ws/daemon` (or `wss://staging-api.savecraft.gg/ws/daemon` for staging) with bearer token in header. The WebSocket connection is authenticated via API key and requires the source to be linked to a user, since the SourceHub DO is keyed by user UUID. (Push API uses the source token `sct_*` separately.)
2. On connect success, send `source_online` event.
3. Listen for incoming messages (config updates, rescan commands) in a goroutine.
4. Send status events as they occur (parse results, errors, game detection).
5. On disconnect, reconnect with exponential backoff: 1s → 2s → 4s → 8s → ... → 60s cap.
6. On graceful shutdown (SIGTERM), send `source_offline` event, close connection.

**Graceful degradation:** If the WebSocket is down, the daemon continues operating locally — watching files, parsing saves, queuing push API calls. Status events are dropped (not queued) during disconnection. The push API (HTTP POST) is independent of the WebSocket; save data always reaches R2 even if the real-time channel is down.

## Local API (`internal/localapi`)

The daemon exposes a localhost HTTP API on port 9182 for tray-to-daemon IPC. The server starts early in the boot sequence so endpoints are available before registration completes.

**Endpoints:**
- `GET /boot` — Returns daemon lifecycle state (starting, registering, registered, running, error)
- `GET /link` — Returns the 6-digit link code and URL during first-boot registration (503 before registration, 404 after linking)
- `GET /logs` — Returns the last 1000 log entries from the in-memory ring buffer
- `GET /status` — Returns daemon runtime status (connected games, sync activity). Added after subsystem initialization.
- `POST /shutdown` — Triggers graceful daemon shutdown
- `POST /restart` — Triggers service restart via `svcmgr.Control`

**Ring buffer:** A `slog.Handler` wrapper (`RingBuffer`) that captures log records into a fixed-size circular buffer while forwarding to the underlying handler. Thread-safe. Entries are returned as structured JSON with timestamp, level, message, and attributes.

## Service Manager (`internal/svcmgr`)

Cross-platform service management replacing `kardianos/service`. The daemon binary handles its own service lifecycle via `savecraftd install`, `savecraftd start`, `savecraftd stop`, and `savecraftd uninstall` subcommands.

**Platform backends:**
- **Linux:** systemd user units (`~/.config/systemd/user/`) with security hardening (ProtectSystem, ProtectHome, ReadWritePaths, NoNewPrivileges, RestrictAddressFamilies)
- **macOS:** launchd user agents (`~/Library/LaunchAgents/`) with KeepAlive and RunAtLoad
- **Windows:** Registry `HKCU\Run` key for auto-start. Stop uses `taskkill`. Restart is not supported (user must stop/start manually).

The `Program` type wraps a `RunFunc` with context cancellation, WaitGroup-based goroutine tracking, and signal handling. `svcmgr.Run()` starts the program, waits for SIGINT/SIGTERM, stops gracefully, and returns any error from the run function.

## Tray Application (`cmd/savecraft-tray`)

A separate binary that displays daemon status in the system tray. Communicates with the daemon exclusively via the local API (no shared memory, no IPC pipes).

**Features:**
- Polls `GET /boot` every 3 seconds to display daemon state
- Copy Logs: fetches `GET /logs` and copies formatted entries to clipboard
- Restart Daemon: sends `POST /restart` to trigger service restart
- Open Dashboard: opens the Savecraft web frontend in the default browser
- Quit: exits the tray app (does not stop the daemon)

**Platform notes:**
- Linux: uses pure Go dbus backend (no CGO). Clipboard uses `wl-copy` on Wayland, `xclip` on X11.
- macOS: requires CGO for Cocoa integration.
- Windows: uses WinAPI. `-H=windowsgui` linker flag suppresses console window.
