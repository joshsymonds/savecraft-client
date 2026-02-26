# Savecraft

Savecraft parses video game save files and exposes structured game state to AI assistants via [MCP](https://modelcontextprotocol.io/) (Model Context Protocol). It enables AI conversations like "what's my Hammerdin's gear?" or "am I on track for Perfection in Stardew?" by giving Claude, ChatGPT, and Gemini access to actual save file data.

**Domains:** savecraft.gg (primary), savecraft.ai (redirect)

## Architecture

Two components that share a user account and a data contract:

1. **Local Daemon** (Go) — watches save file directories, parses saves using WASM plugins (wazero), and pushes structured JSON to the cloud API. Maintains a WebSocket connection for real-time config updates and status reporting.

2. **Remote MCP Server** (Cloudflare Worker) — serves game state to AI clients via MCP tools. OAuth via Clerk. Single Worker handles the push API, MCP endpoint, and Durable Object hub.

See [docs/savecraft-architecture.md](docs/savecraft-architecture.md) for the full architecture document.

## Repository Structure

Monorepo. Single Go module.

```
savecraft/
├── cmd/
│   ├── daemon/          # Local daemon binary
│   └── server/          # MCP server + push API binary
├── internal/            # Shared packages (schema, storage, auth, plugin, mcp, etc.)
├── worker/              # Cloudflare Worker + Durable Object (TypeScript)
├── plugins/             # WASM plugin sources (D2R, Stardew, etc.)
├── install/             # Platform installers and systemd units
├── web/                 # Auth pages, settings UI, device status dashboard
└── docs/                # Architecture docs and wireframes
```

## Game Support

- **v1:** Diablo II: Resurrected (.d2s)
- **Next:** Stardew Valley (XML), Paradox games (Clausewitz)

## Development

Coming soon.
