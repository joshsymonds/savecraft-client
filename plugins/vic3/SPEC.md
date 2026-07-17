# Victoria 3 parser status

Victoria 3 is a work-in-progress local parser. It intentionally has no
`plugin.toml` and is not included in the released plugin registry.

The Rust crate builds for `wasm32-wasip1` and shares ZIP-envelope and NDJSON
support with Stellaris through `libs/clausewitz-core`. It currently validates
the save envelope, reports progress, and returns a structured
`not_implemented` error; Clausewitz gamestate extraction is still pending.

```bash
cd plugins/vic3
just build
just test
```

Before release, the parser must implement and test save identity, summary, and
object-valued sections against supported text and binary saves. Only then
should the project add client metadata and enter the signed plugin registry.
