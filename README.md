# Savecraft Client

Savecraft connects local game state to the hosted Savecraft product. This
repository contains every component Savecraft distributes to or installs on a
user's machine:

- the `savecraftd` daemon and system-tray client;
- the installer, service integration, self-updater, and Windows MSI sources;
- the wazero-based WASM plugin runtime and all bundled save-file parser plugins;
- the Factorio Lua mod and RimWorld C# mod.

The sole hosted exception in this repository is [`install/worker`](install/worker/),
which distributes client installers and updates. The hosted Savecraft product
is proprietary and developed elsewhere.

## Local data flow

The daemon runs locally. It reads save, log, or mod-export files only from save
directories selected by the user, then runs the matching parser locally in a
WASM sandbox. The tray communicates with the daemon over localhost. The
Factorio and RimWorld integrations run inside their respective games.

Raw save files stay on the user's machine. The client sends parsed game state
and operational messages to the hosted service. The exact wire egress schema is
[`proto/savecraft/v1/protocol.proto`](proto/savecraft/v1/protocol.proto).

## Repository layout

```text
cmd/                 daemon, tray, signing, and registry commands
internal/            daemon, updater, plugin runtime, and client libraries
proto/               client/server wire protocol
plugins/             bundled parsers and in-game mods
libs/                 shared parser libraries
install/              installers, service assets, MSI, and distribution Worker
docs/                 client architecture and plugin development
```

## Development

The Nix devenv supplies Go, Rust, WASI, buf/protoc, Node.js, and Wrangler.

```bash
direnv allow
just test-go
just lint-go
just build-plugin d2r
just proto
```

See [the daemon documentation](docs/daemon.md) and
[plugin development guide](docs/plugin-development.md) for details.

## License

This repository is licensed under the [Apache License 2.0](LICENSE). Existing
Apache-licensed releases remain available under, and continue to be governed
by, the terms under which they were released.
