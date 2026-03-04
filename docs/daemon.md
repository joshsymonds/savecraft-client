# Daemon

## Architecture: Interface-Driven with Fakes

The daemon orchestrator (`internal/daemon/`) defines interfaces for all external dependencies: `Watcher`, `Runner`, `PushClient`, `WSClient`, `FS`, `PluginManager`. Tests inject hand-written fakes. Real implementations live in separate packages (`internal/runner/`, `internal/watcher/`, etc.) and satisfy the interfaces implicitly.

The `Daemon.Run()` loop: register device (if first boot) → connect WebSocket → send `DaemonOnline` → scan configured games → enter event loop (file events, WS commands, context cancellation). On shutdown, send `DaemonOffline`.

## First-Boot Device Registration

On first boot, the daemon has no device token. It self-registers by calling `POST /api/v1/device/register` (unauthenticated). The server returns a `dvt_*` device token, device UUID, and a 6-digit link code (20-minute TTL).

The daemon:
1. Persists the device token and UUID to local config (`~/.config/savecraft/device.json`)
2. Displays the 6-digit link code to the user (CLI output, tray notification, or terminal banner)
3. Polls `GET /api/v1/device/status` periodically to check if the user has linked the device
4. If the link code expires before linking, calls `POST /api/v1/device/link-code` to get a fresh code

On subsequent boots, the daemon reads the persisted token and skips registration. The push API and WebSocket both authenticate with this device token.

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

1. On startup, after sending `DaemonOnline`, daemon calls `PluginManager.Manifests()` to get the full plugin manifest.
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
1. On startup, connect to `wss://api.savecraft.gg/ws/daemon` (or `wss://staging-api.savecraft.gg/ws/daemon` for staging) with bearer token in header. The WebSocket connection is authenticated via API key and requires the device to be linked to a user, since the DaemonHub DO is keyed by user UUID. (Push API uses the device token `dvt_*` separately.)
2. On connect success, send `daemon_online` event.
3. Listen for incoming messages (config updates, rescan commands) in a goroutine.
4. Send status events as they occur (parse results, errors, game detection).
5. On disconnect, reconnect with exponential backoff: 1s → 2s → 4s → 8s → ... → 60s cap.
6. On graceful shutdown (SIGTERM), send `daemon_offline` event, close connection.

**Graceful degradation:** If the WebSocket is down, the daemon continues operating locally — watching files, parsing saves, queuing push API calls. Status events are dropped (not queued) during disconnection. The push API (HTTP POST) is independent of the WebSocket; save data always reaches R2 even if the real-time channel is down.
