# Parser plugins

Savecraft parser plugins run locally inside the daemon. Each game lives under
`plugins/{game_id}/` and supplies a `Justfile` whose `build` recipe produces
`parser.wasm`. RimWorld is the exception: its C# integration runs as an
in-game mod and does not produce parser WASM.

## Sandbox

The daemon executes WASI Preview 1 modules with wazero. A parser receives raw
save bytes on stdin and can write only stdout and stderr. It has no filesystem,
network, or environment access. The daemon reads the user-selected file and
provides those bytes to the sandbox; the plugin never opens the file itself.

Released parser binaries are signed with Ed25519. The daemon verifies each
signature against its embedded public key before execution.

## NDJSON contract

Plugins write one JSON object per line to stdout:

- `{"type":"status","message":"..."}` reports optional progress.
- `{"type":"result","identity":{...},"summary":"...","sections":{...}}`
  is the final line for a successful parse.
- `{"type":"error","errorType":"parse_error","message":"..."}` is the
  final line for a failed parse.

Valid error types are `unsupported_version`, `corrupt_file`, and
`parse_error`. stderr is reserved for diagnostics.

Each entry in `sections` contains a string `description` and an object-valued
`data` field. Arrays and scalar values must be nested under a descriptive key:

```json
{
  "equipment": {
    "description": "Equipped items",
    "data": {"items": [{"slot": "head", "name": "Example"}]}
  }
}
```

The daemon rejects non-object section data and caps a result line at 2 MiB.

## Metadata

The eight released client plugins have a `plugin.toml`. Victoria 3 is a
work-in-progress parser and intentionally has no manifest yet. Public manifests
contain only client fields:

```toml
game_id = "d2r"
sources = ["wasm"]
icon = "icon.png"
name = "Diablo II: Resurrected"
description = "Parses local character and stash saves"
channel = "beta"
coverage = "partial"
file_extensions = [".d2s", ".d2i"]
file_patterns = []
exclude_dirs = []
homepage = "https://savecraft.gg/plugins/d2r"
limitations = ["Only supported save versions are accepted"]

[attribution]
sources = ["blizzard"]

[author]
name = "Josh Symonds"
github = "joshsymonds"

[default_paths]
windows = "%SAVED_GAMES%/Diablo II Resurrected"
linux = "~/.local/share/Diablo II Resurrected"
darwin = "~/Library/Application Support/Diablo II Resurrected"
```

The daemon expands `~` and environment placeholders. A user-selected path
overrides the suggested default.

## Build and release descriptors

Build a parser and generate its client-only descriptor with:

```bash
just build-plugin d2r
just plugin-registry d2r dev
```

`cmd/plugin-registry` reads `plugin.toml`, hashes `parser.wasm`, and writes
`client-manifest.json`. CI builds and signs parser WASM, uploads the per-game
descriptor, then composes all published descriptors into the signed aggregate
served as `/plugins/manifest.json`. The aggregate's literal bytes are signed;
the daemon verifies that signature before parsing any URL or version.

See [plugin-development.md](plugin-development.md) for the local development
loop.
