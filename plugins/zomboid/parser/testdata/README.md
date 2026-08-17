# Project Zomboid fixture provenance

These files are the owner's own world, captured on 2026-08-16 from `gnomon`
using the native Linux Project Zomboid Build 42.20.2 build
(`revision=ffe7a8a4b1`). The world was created in Tutorial mode and is saved
as `Zomboid/Saves/Tutorial/1789228836736670246/`.

The fixture contains no platform IDs, credentials, or server details:
`networkPlayers` has zero rows and `mods.txt` contains empty `mods` and `maps`
lists. Owner sign-off is recorded in the epic.

| File | SHA-256 | Size | Contents |
| --- | --- | ---: | --- |
| `tutorial-42.20.2/players.db` | `d7931fd0e36586863577a9552be5d25766a40069ec1340e14fff227c55c68fcc` | 24,576 B | `localPlayers`: one row, id 1 `Jane Doe`, wx 22, wy 18, x 178.5625, y 147.0225, z 0, worldversion 249, data 4,312 B, `isDead` 0; `networkPlayers`: 0 rows; SQLite page size 4096, page count 6 |
| `tutorial-42.20.2/WorldDictionaryReadable.lua` | `716c7bedcaa2f06370c8124c910d2e91da86201c34db27b7c474a60327cf35e2` | 953,513 B | `ITEMS` section with 5,092 records |
| `tutorial-42.20.2/mods.txt` | `1dbba53e64c2efe0254b70a9a3befc1cf75243ecbd800e6ce832ea6554197c73` | 33 B | `VERSION = 1`, `mods { }`, `maps { }` |
| `jane-doe-249.blob` | `de011b2d120dab1a945ddf860a1bdc42537c3a1f377279d3c9f78a78557ccc70` | 4,312 B | Verbatim `localPlayers.data` BLOB from the Jane Doe row |

Verify the committed bytes with:

```bash
sha256sum tutorial-42.20.2/players.db \
  tutorial-42.20.2/WorldDictionaryReadable.lua \
  tutorial-42.20.2/mods.txt jane-doe-249.blob
```
