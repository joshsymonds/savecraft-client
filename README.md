<p align="center">
  <img src="assets/icon.png" width="180" alt="Savecraft" />
</p>

<h1 align="center">Savecraft</h1>

<p align="center">
  <strong>Your saves, your AI, your edge.</strong>
</p>

<p align="center">
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/ci.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-daemon.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-daemon.yml/badge.svg" alt="Daemon Deploy" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-plugin.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-plugin.yml/badge.svg" alt="Plugin Deploy" /></a>
</p>

<p align="center">
  Savecraft gives AI assistants access to your actual game state via <a href="https://modelcontextprotocol.io/">MCP</a>.<br />
  Player 2 for every game you play alone.
</p>

---

Most games worth caring about are played solo. The best moments — the lucky drop, the build finally clicking, the boss that took twenty attempts — happen with nobody watching. Savecraft means there's always someone who knows the context. You don't explain what a Shael is or why Perfection matters. The AI already knows your character, your gear, your goals.

**Optimize:** "Am I hitting the 125% FCR breakpoint?" / "Compare my build to the Maxroll guide."

**Talk:** "Another Countess run and ZERO SHAELS." / "I JUST FOUND A BER RUNE."

Same tools, same data. The conversation goes wherever you take it.

## How It Works

```
  ┌─────────────────────┐            ┌───────────────────────────┐
  │  Gaming Device       │   HTTPS    │  Cloudflare               │
  │                      │  ───────>  │                           │
  │  savecraftd          │   push     │  Push API ──> R2 Storage  │
  │  - fs watcher        │            │                           │
  │  - WASM plugin       │  <──────>  │  DaemonHub DO (WebSocket) │
  │    runtime (wazero)  │    WS      │                           │
  └─────────────────────┘            │  MCP Server ──> AI Tools  │
                                      └───────────────────────────┘
  ┌─────────────────────┐                        │
  │  Claude / ChatGPT /  │  <──── MCP ──────────┘
  │  Gemini              │
  └─────────────────────┘
```

1. **Daemon watches** your save files. Detects changes via fsnotify with debounce + hash dedup.
2. **WASM plugins parse** saves into structured JSON. Sandboxed via wazero — plugins can't touch your filesystem or network. Ed25519 signed.
3. **AI reads your state** through MCP tools. Section-level granularity — the AI fetches only what it needs to answer your question.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Daemon | Go, wazero (WASM runtime), fsnotify, nhooyr.io/websocket |
| Cloud | Cloudflare Workers, Durable Objects, R2, D1 (SQLite/FTS5) |
| Auth | Clerk (OAuth, JWT, magic links) |
| Frontend | SvelteKit, TypeScript |
| Plugins | Go compiled to WASI Preview 1, ndjson stdout contract |
| Protocol | Protobuf (buf codegen to Go + TypeScript) |
| Build | just, nix devenv + direnv |

## MCP Tools

| Tool | Description |
|------|-------------|
| `list_games` | All games with saves, note titles, and reference modules with parameter schemas |
| `get_save` | Summary, overview, sections, and notes for a save |
| `get_section` | Section data (optional historical timestamp) |
| `get_section_diff` | Changes between two snapshots |
| `refresh_save` | Request fresh data (daemon-backed or API-backed) |
| `search_saves` | Full-text search across all saves and notes |
| `get_note` | Full content of a user-attached note |
| `create_note` / `update_note` / `delete_note` | Manage notes via AI conversation |
| `query_reference` | Execute reference data computations (drop rates, build math) |

## Plugins

Plugins are WASM binaries that parse game save files. They read raw bytes on stdin, emit ndjson on stdout, and cannot access the filesystem or network. Each plugin is Ed25519 signed and verified before loading.

| Game | Format | Status | Author |
|------|--------|--------|--------|
| [Diablo II: Resurrected](plugins/d2r/) | `.d2s` binary | Beta | [@joshsymonds](https://github.com/joshsymonds) |

**Planned:** Stardew Valley (XML), Stellaris/CK3 (Clausewitz), Baldur's Gate 3 (.lsv), Elden Ring (.sl2), Civilization VI

Server-side adapters (no daemon required) planned for API-backed games: Path of Exile 2, WoW (Battle.net API), FFXIV.

### Writing a Plugin

Plugins speak a simple contract:

```
stdin:  raw save file bytes
stdout: ndjson lines — {"type": "status"|"result"|"error", ...}
```

Write a parser in Go (or Rust, Zig, anything targeting WASI Preview 1), compile to `.wasm`, add a `plugin.toml` with metadata. See [`plugins/echo/`](plugins/echo/) for a minimal reference and [`plugins/d2r/`](plugins/d2r/) for a real-world parser.

## Project Structure

```
savecraft.gg/
├── cmd/savecraftd/       # Daemon entrypoint
├── internal/
│   ├── daemon/           # Orchestrator, domain types, interfaces
│   ├── runner/           # WASM plugin execution (wazero)
│   ├── watcher/          # Filesystem watcher (fsnotify + debounce)
│   ├── push/             # HTTP push client
│   ├── wsconn/           # WebSocket client (reconnecting)
│   └── pluginmgr/        # Plugin download, verification, caching
├── worker/               # Cloudflare Worker + Durable Object (TypeScript)
├── web/                  # SvelteKit frontend
├── plugins/              # WASM plugin sources
├── proto/                # Protobuf protocol definitions
├── install/              # Platform installers + systemd units
├── assets/               # Brand assets
└── docs/                 # Architecture docs + wireframes
```

## Development

Requires nix devenv + direnv. `direnv allow` activates the environment on `cd`.

```bash
just --list          # Show all targets
just test            # Run all tests (Go + Worker)
just check           # Lint, generate, test everything
just proto           # Regenerate Go + TypeScript from protobuf
just dev-worker      # Start Worker dev server (Miniflare)
```

See [`docs/overview.md`](docs/overview.md) for the system architecture, or browse `docs/` for component-specific documentation.

## Security

- **WASM sandboxed:** Plugins cannot access filesystem, network, or environment. stdin in, JSON out.
- **Ed25519 signed:** Every plugin binary is cryptographically signed. Tampered = refused.
- **Read-only daemon:** Cannot modify your saves. Kernel-enforced on Linux via systemd sandboxing.
- **No filesystem exposure:** AI sees structured JSON, never your local paths or files.
- **Private R2:** No public bucket access. The Worker mediates all reads/writes, scoped to the authenticated user.

## License

Proprietary. All rights reserved.

---

<p align="center">
  <sub>savecraft.gg — by <a href="https://joshsymonds.com">@joshsymonds</a></sub>
</p>
