<p align="center">
  <img src="assets/icon.png" width="180" alt="Savecraft" />
</p>

<h1 align="center">Savecraft</h1>

<p align="center">
  <strong>MCP server for game save files.</strong>
</p>

<p align="center">
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-cloud.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-cloud.yml/badge.svg" alt="Cloud Deploy" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-daemon.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-daemon.yml/badge.svg" alt="Daemon Deploy" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-plugin.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-plugin.yml/badge.svg" alt="Plugin Deploy" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-install.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/deploy-install.yml/badge.svg" alt="Install Deploy" /></a>
  <a href="https://github.com/joshsymonds/savecraft.gg/actions/workflows/test-windows.yml"><img src="https://github.com/joshsymonds/savecraft.gg/actions/workflows/test-windows.yml/badge.svg" alt="Windows Tests" /></a>
</p>

<p align="center">
  <video src="https://github.com/user-attachments/assets/a44e59dd-b622-413f-a27a-6670cf51d74a" />
</p>

Savecraft watches your game saves, parses them into structured data, and serves it to AI assistants via [MCP](https://modelcontextprotocol.io/). Claude, ChatGPT, or Gemini can read your characters, gear, skills, and progress - real data, updated live.

---

## Quick Start

**1. Install the daemon**

Linux / Steam Deck:
```bash
curl -sSL https://install.savecraft.gg | bash
```

Windows / Mac: download from [install.savecraft.gg](https://install.savecraft.gg)

**2. Connect your AI**

Sign in at [my.savecraft.gg](https://my.savecraft.gg) to get your MCP connector URL, then add it to your AI assistant's MCP settings ([Claude](https://claude.ai), [ChatGPT](https://chatgpt.com), [Gemini](https://gemini.google.com)).

The daemon auto-detects supported games and starts syncing. Ask your AI about your character, your build, or your last run - it already has the data.

## How It Works

```
  ┌─────────────────────┐            ┌───────────────────────────┐
  │  Gaming Device      │   HTTPS    │  Cloudflare               │
  │                     │  ───────>  │                           │
  │  savecraftd         │   push     │  Push API ──> R2 Storage  │
  │  - fs watcher       │            │                           │
  │  - WASM plugin      │  <──────>  │  SourceHub DO (WebSocket) │
  │    runtime (wazero) │    WS      │                           │
  └─────────────────────┘            │  MCP Server ──> AI Tools  │
                                     └───────────────────────────┘
  ┌─────────────────────┐                       │
  │  Claude / ChatGPT / │   <──── MCP ──────────┘
  │  Gemini             │
  └─────────────────────┘
```

1. **Daemon watches** your save files. Detects changes via fsnotify with debounce + hash dedup.
2. **WASM plugins parse** saves into structured JSON. Sandboxed via wazero - plugins can't touch your filesystem or network. Ed25519 signed.
3. **AI reads your state** through MCP tools. Section-level granularity - the AI fetches only what it needs to answer your question.

## MCP Tools

| Tool | Description |
|------|-------------|
| `list_games` | All games with saves, note titles, and reference modules with parameter schemas |
| `get_save` | Summary, overview, sections, and notes for a save |
| `get_section` | Section data from D1 |
| `refresh_save` | Request fresh data (daemon-backed or API-backed) |
| `search_saves` | Full-text search across all saves and notes |
| `get_note` | Full content of a user-attached note |
| `create_note` / `update_note` / `delete_note` | Manage notes via AI conversation |
| `query_reference` | Execute reference data computations (drop rates, build math) |

## Supported Games

Plugins are sandboxed WASM binaries that parse save files. They read raw bytes on stdin, emit structured JSON on stdout, and cannot access your filesystem or network. Each plugin is Ed25519 signed and verified before loading. Plugins can optionally ship a `reference.wasm` for server-side computation: drop calculators, gift databases, crop planners - deployed via Workers for Platforms.

| Game | Format | Reference Modules | Status | Author |
|------|--------|-------------------|--------|--------|
| [Clair Obscur: Expedition 33](plugins/clair-obscur/) | Save file (WASM) | — | Beta | [@joshsymonds](https://github.com/joshsymonds) |
| [Diablo II: Resurrected](plugins/d2r/) | `.d2s` / `.d2i` binary | Drop Calculator | Beta | [@joshsymonds](https://github.com/joshsymonds) |
| [Factorio](plugins/factorio/) | [Lua mod](plugins/factorio/mod/) + WASM | Recipe Lookup, Ratio Calculator, Oil Balancer, Tech Tree, Blueprint Analyzer, Evolution Tracker, Power Calculator, Production Flow | Alpha | [@joshsymonds](https://github.com/joshsymonds) |
| [Magic: The Gathering Arena](plugins/mtga/) | `Player.log` | Card Search, Rules Search, Draft Advisor, Play Advisor, Card Stats, Deckbuilding, Collection Diff, Match Stats, Sideboard Analysis, Mana Base | Beta | [@joshsymonds](https://github.com/joshsymonds) |
| [RimWorld](plugins/rimworld/) | [Steam Workshop mod](https://steamcommunity.com/sharedfiles/filedetails/?id=3693580596) | Surgery Calculator, Crop Optimizer, Combat Calculator, Material Lookup, Drug Analyzer, Raid Estimator, Gene Builder, Research Navigator | Beta | [@joshsymonds](https://github.com/joshsymonds) |
| [Stardew Valley](plugins/sdv/) | XML save directory | Gift Preferences, Crop Planner | Beta | [@joshsymonds](https://github.com/joshsymonds) |
| [Stellaris](plugins/stellaris/) | `.sav` (Clausewitz/Rust) | Tech Search, Building Search, Component Search, Tradition Search, Trait Search, Civic Search, Edict Search, Job Search | Alpha | [@joshsymonds](https://github.com/joshsymonds) |
| [World of Warcraft](plugins/wow/) | Battle.net API | — | Beta | [@joshsymonds](https://github.com/joshsymonds) |

See [`docs/games.md`](docs/games.md) for detailed descriptions of each game's sections and reference modules.

**Planned save-file parsers:** Victoria 3 (Clausewitz/Rust), CK3/HOI4 (Clausewitz), Baldur's Gate 3 (.lsv), Elden Ring (.sl2), Civilization VI, Bethesda games (.ess)

**Planned API adapters** (no daemon required): Path of Exile 2, FFXIV

**Planned mod integrations:** Minecraft, Terraria (mod-as-device: mod pushes directly, no daemon)

Want to add a game? See the [plugin development guide](docs/plugin-development.md).

## Project Structure

```
savecraft.gg/
├── cmd/savecraftd/       # Daemon entrypoint
├── internal/
│   ├── daemon/           # Orchestrator, domain types, interfaces
│   ├── runner/           # WASM plugin execution (wazero)
│   ├── watcher/          # Filesystem watcher (fsnotify + debounce)
│   ├── wsconn/           # WebSocket client (reconnecting)
│   ├── pluginmgr/        # Plugin download, verification, caching
│   ├── selfupdate/       # Daemon self-update mechanism
│   └── signing/          # Ed25519 plugin signature verification
├── worker/               # Cloudflare Worker + Durable Object (TypeScript)
├── reference/            # Reference Worker - WASI shim for server-side plugin computation (WfP)
├── web/                  # SvelteKit frontend
├── plugins/              # WASM plugin sources (parser + optional reference per game)
├── proto/                # Protobuf protocol definitions
├── install/              # Platform installers + systemd units
├── assets/               # Brand assets
└── docs/                 # Architecture docs
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

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Daemon | Go, wazero (WASM runtime), fsnotify, nhooyr.io/websocket |
| Cloud | Cloudflare Workers, Durable Objects, R2, D1 (SQLite/FTS5) |
| Auth | Clerk (OAuth, JWT, magic links) |
| Frontend | SvelteKit, TypeScript |
| Plugins | Go or Rust compiled to WASI Preview 1, ndjson stdout contract |
| Protocol | Protobuf (buf codegen to Go + TypeScript) |
| Build | just, nix devenv + direnv |

## Security

- **WASM sandboxed:** Plugins cannot access filesystem, network, or environment. stdin in, JSON out.
- **Ed25519 signed:** Every plugin binary is cryptographically signed. Tampered = refused.
- **Read-only daemon:** Cannot modify your saves. Kernel-enforced on Linux via systemd sandboxing.
- **No filesystem exposure:** AI sees structured JSON, never your local paths or files.
- **Private R2:** No public bucket access. The Worker mediates all reads/writes, scoped to the authenticated user.

## License

[Apache License 2.0](LICENSE)

---

<p align="center">
  <sub>savecraft.gg - by <a href="https://joshsymonds.com">@joshsymonds</a> · <a href="https://savecraft.gg/privacy">Privacy Policy</a></sub>
</p>
