# Test fixtures

`1FCE97C34D214643B96A23A20A9E27D1/` is a real Palworld dedicated-server world
save directory, captured from the plugin author's own save (Josh Symonds).
Publishing it here is intentional — it exists specifically to drive this
plugin's tests against real save data.

- Game build: Steam depot build 24467282
- Capture date: 2026-07-30
- World name: "Palpagos Islands"
- Layout: world-id directory only — no `SteamID64` segment under `Players/`
  in this capture

## Files

```
1FCE97C34D214643B96A23A20A9E27D1/
├── Level.sav
├── LevelMeta.sav
├── LocalData.sav
├── WorldOption.sav
└── Players/
    └── 00000000000000000000000000000001.sav
```

## SHA-256 checksums

| File | SHA-256 |
| --- | --- |
| `Level.sav` | `f13144edb13528c26f5624f0165e77135dba260f3665cec23a184ffa703de06e` |
| `LevelMeta.sav` | `38ab4b029b9c840a9b4da3c28d45ca42b58512b940cf548d77b1ade55516cde9` |
| `LocalData.sav` | `e6917384a17dc72e2c377b67d2bcf80692c0884a41481f005eaa8bd1dd513eda` |
| `WorldOption.sav` | `45c4833617c8d106b2d1f8b7dd61a6c6942cbf4afa241b0593ec528f0a73b022` |
| `Players/00000000000000000000000000000001.sav` | `84e49ed63f2ab4eb77a1f832784cea0f7c4b2a000db7844beb8b7e6b42d30dcc` |

Verify with:

```sh
sha256sum -c <<'EOF'
f13144edb13528c26f5624f0165e77135dba260f3665cec23a184ffa703de06e  1FCE97C34D214643B96A23A20A9E27D1/Level.sav
38ab4b029b9c840a9b4da3c28d45ca42b58512b940cf548d77b1ade55516cde9  1FCE97C34D214643B96A23A20A9E27D1/LevelMeta.sav
e6917384a17dc72e2c377b67d2bcf80692c0884a41481f005eaa8bd1dd513eda  1FCE97C34D214643B96A23A20A9E27D1/LocalData.sav
45c4833617c8d106b2d1f8b7dd61a6c6942cbf4afa241b0593ec528f0a73b022  1FCE97C34D214643B96A23A20A9E27D1/WorldOption.sav
84e49ed63f2ab4eb77a1f832784cea0f7c4b2a000db7844beb8b7e6b42d30dcc  1FCE97C34D214643B96A23A20A9E27D1/Players/00000000000000000000000000000001.sav
EOF
```
