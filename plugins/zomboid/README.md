# Savecraft for Project Zomboid

This plugin reads Project Zomboid Build 42 save directories directly. It
extracts the character sheet from the `localPlayers` row in `players.db` and
resolves item ids through `WorldDictionaryReadable.lua`.

This release accepts world version 249 (Build 42.20.x) only. Build 41 saves
are rejected as unsupported, and unknown serialized character data fails
closed. The parser currently reports skills, XP, traits, a health summary,
and worn and carried items. Vitals, vehicles, and base contents are not yet
included.

## What the character section contains

- identity, profession, gender, and alive/dead state
- health summary and body-part injuries
- position, survival time, zombie and survivor kills
- perks, XP, traits, and XP boosts
- nutrition, worn items, and carried item counts
- active mods and other characters in the save

## Save layout and members

The daemon's default paths are:

```text
Linux/macOS: ~/Zomboid/Saves/*/*
Windows:     %USERPROFILE%/Zomboid/Saves/*/*
```

Build 42 singleplayer and locally hosted worlds use
`Saves/<Mode>/<SaveName>/`. The fixture was captured from
`Saves/Tutorial/1789228836736670246/`; an Apocalypse world follows the same
shape. The directory-unit archive includes these members:

| Member | Required | Purpose |
| --- | --- | --- |
| `players.db` | yes | `localPlayers` character rows and their serialized blob |
| `WorldDictionaryReadable.lua` | yes | readable item dictionary |
| `players.db-journal` | no | SQLite journal when present |
| `mods.txt` | no | save mod/version metadata |

Files under `map/` and all other world files are intentionally not archived.

Steam Proton users can select this directory manually in Savecraft (it is not
a default path):

```text
~/.local/share/Steam/steamapps/compatdata/108600/pfx/drive_c/users/steamuser/Zomboid/Saves/*/*
```

## Multiplayer investigation

The B42 layout references in the [PZwiki game-files
guide](https://pzwiki.net/wiki/Game_files) and [save
guide](https://pzwiki.net/wiki/Save) put local saves below
`Zomboid/Saves/<Mode>/<SaveName>/`. A remote client cache is written below
`Zomboid/Saves/Multiplayer/<server-address>_<username>/`.

We checked the B42 savefile classes [`PlayerDB`](https://github.com/piromasta/PZDecompiled/blob/main/zombie/savefile/PlayerDB.java),
[`ClientPlayerDB`](https://github.com/piromasta/PZDecompiled/blob/main/zombie/savefile/ClientPlayerDB.java),
and [`ServerPlayerDB`](https://github.com/piromasta/PZDecompiled/blob/main/zombie/savefile/ServerPlayerDB.java)
alongside the fixture's SQLite schema. `PlayerDB` creates and reads
`localPlayers`; a remote client keeps its character in `ClientPlayerDB`'s
server profile, while the server persists network characters through
`ServerPlayerDB`'s `networkPlayers`. The remote cache therefore does not
provide a usable `localPlayers` character row. Feeding those caches to this
parser would produce a `parse_error` without useful character data, so
`exclude_dirs = ["Multiplayer"]` is deliberate.

`exclude_dirs` is applied by the daemon while expanding each glob segment,
not only while walking members. The daemon test
`TestDiscoverGames_ZomboidDirectoryLayout` pins that `Tutorial` and
`Apocalypse` resolve as saves, `Multiplayer` does not, and only the four
listed members are collected. The dictionary/spec provenance is the pinned
[`cff29546/pzdataspec` v1.12.249 source](https://github.com/cff29546/pzdataspec/tree/2a343e4c3dc942d451e1741895737a3a0efafcfa).

## Build and test

```bash
cd plugins/zomboid
just build
just test
```

`just build` produces `parser.wasm`; `just test` runs the parser package tests
with Go's `jsonv2` experiment enabled.
