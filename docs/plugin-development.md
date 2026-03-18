# Plugin Development Guide

How to build, test, and iterate on Savecraft WASM plugins locally. For the plugin system architecture and ndjson contract, see [plugins.md](plugins.md).

## Prerequisites

- Go toolchain (1.26+) or the nix devenv (`direnv allow` in repo root)
- A running daemon binary (build with `just build-daemon` or use a release)
- A staging account at `staging-my.savecraft.gg` (register via the daemon's link flow)

## Building a Plugin

Each plugin lives in `plugins/{game_id}/` with its own `Justfile`. Build one:

```bash
just build-plugin d2r
```

This compiles the plugin to `plugins/d2r/parser.wasm`. Build all plugins:

```bash
just build-plugins
```

## Local Development Setup

### Directory Structure

Set `SAVECRAFT_PLUGIN_DIR` to a directory containing per-game subdirectories:

```
/path/to/plugins/
  d2r/
    parser.wasm
  rimworld/
    parser.wasm
```

The daemon loads `{SAVECRAFT_PLUGIN_DIR}/{gameID}/parser.wasm` for each game. If a game isn't present in the local directory, the daemon falls through to downloading the published version from the server.

The simplest setup is to point directly at the repo's `plugins/` directory:

```bash
export SAVECRAFT_PLUGIN_DIR=$PWD/plugins
```

### Environment Variables

| Variable | Value | Purpose |
|---|---|---|
| `SAVECRAFT_PLUGIN_DIR` | Path to local plugins dir | Load plugins from disk instead of downloading |
| `SAVECRAFT_SKIP_VERIFY` | `1` | Skip Ed25519 signature verification (local builds aren't signed) |
| `SAVECRAFT_SERVER_URL` | `https://staging-api.savecraft.gg` | Connect to staging, not production |

### Running the Daemon

```bash
SAVECRAFT_PLUGIN_DIR=$PWD/plugins \
SAVECRAFT_SKIP_VERIFY=1 \
SAVECRAFT_SERVER_URL=https://staging-api.savecraft.gg \
  go run ./cmd/savecraftd run
```

Or build first and run the binary:

```bash
just build-daemon linux amd64 dev https://staging-api.savecraft.gg https://staging-install.savecraft.gg savecraft-dev 19683 https://staging-my.savecraft.gg
SAVECRAFT_PLUGIN_DIR=$PWD/plugins SAVECRAFT_SKIP_VERIFY=1 ./dist/savecraft-dev-daemon-linux-amd64
```

## Development Loop

### Auto-Reload (Recommended)

When `SAVECRAFT_PLUGIN_DIR` is set, the daemon watches for `parser.wasm` file changes via fsnotify. The workflow is:

1. Start the daemon with the environment variables above
2. Edit your plugin code
3. Run `just build-plugin {game_id}`
4. The daemon detects the new `parser.wasm`, reloads it, and re-parses all tracked saves for that game
5. Check the results via MCP tools or the web UI at `staging-my.savecraft.gg`

No daemon restart needed. The debounce window is 500ms, so the reload happens almost immediately after the build completes.

### Manual Reload

If you prefer explicit control, use the CLI command while the daemon is running:

```bash
savecraftd update-plugins
```

This checks both local plugins (re-reads from disk, reloads if changed) and remote plugins (fetches manifest, downloads updates). The local directory always takes priority.

## Staging Requirement

**Always develop and test against staging. Never use production for plugin development.**

- Staging URL: `https://staging-api.savecraft.gg`
- Staging web: `https://staging-my.savecraft.gg`
- Staging has its own database, R2 buckets, and Clerk instance — your test data won't affect real users

Production should only receive plugins that have been reviewed, signed, and deployed through CI.

## Testing Your Plugin

### Via MCP

Connect an MCP client (like Claude) to `https://staging-mcp.savecraft.gg` and use the search/get tools to verify your plugin's output.

### Via CLI (Direct Execution)

Test a plugin directly without the daemon:

```bash
cat /path/to/save/file.d2s | go run ./plugins/d2r/
```

This runs the plugin as a standalone WASI executable and prints the ndjson output to stdout.

### Via Unit Tests

Each plugin should have its own test suite. For example:

```bash
go test ./plugins/d2r/...
```

## Troubleshooting

**Plugin not loading:**
- Check directory structure: `{SAVECRAFT_PLUGIN_DIR}/{gameID}/parser.wasm` (not `{SAVECRAFT_PLUGIN_DIR}/parser.wasm`)
- Verify `SAVECRAFT_SKIP_VERIFY=1` is set — local builds aren't Ed25519 signed

**Auto-reload not firing:**
- The daemon only watches subdirectories that exist when it starts. If you add a new game directory, restart the daemon.
- Check daemon logs for "local plugin changed, reloading" messages

**Plugin parses locally but fails via daemon:**
- The daemon feeds raw file bytes on stdin. Ensure your plugin reads from stdin, not from a file path.
- Check stderr output in daemon logs for plugin error details.

**Wrong server:**
- Verify `SAVECRAFT_SERVER_URL` is set to `https://staging-api.savecraft.gg`
- If unset, the daemon uses the compiled-in default (which is production for release builds)
