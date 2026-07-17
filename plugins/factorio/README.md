# Savecraft for Factorio

The Factorio integration runs locally in two parts:

- `mod/control.lua` exports structured game state to
  `script-output/savecraft/state.json`.
- `parser/` validates that export and converts it to Savecraft's NDJSON parser
  contract inside the daemon's WASM sandbox.

The parser does not open files or contact the network. The daemon reads the
user-selected export directory and supplies each JSON document on stdin.

## Build and test

```bash
cd plugins/factorio
just build
just test
```

The build writes `parser.wasm`. Package the in-game mod from the repository
root with `just factorio-mod`.

## Mod installation

The packaged mod contains `info.json`, `control.lua`, the thumbnail, and the
changelog. Enable the mod in Factorio and select its script-output directory in
the Savecraft client. Known coverage limits are recorded in
[`plugin.toml`](plugin.toml).

## Metadata and attribution

`plugin.toml` is the public client descriptor source. Factorio game data and
marks are attributed to Wube Software; see [`mod/PORTAL.md`](mod/PORTAL.md) for
the distribution description.
