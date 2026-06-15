# Satisfactory parser test fixtures

Real save files used as parser test fixtures. Both verified to be game version 1.0+
by reading the first 8 bytes (little-endian int32 SaveHeaderVersion, int32 SaveVersion).
Header metadata below was extracted by parsing the uncompressed save header directly.

## early_game.sav

- **Original filename:** `Release-032.sav`
- **Source:** https://raw.githubusercontent.com/etothepii4/satisfactory-file-parser/main/src/test/Release-032.sav
  (test fixture in [etothepii4/satisfactory-file-parser](https://github.com/etothepii4/satisfactory-file-parser), MIT licensed)
- **File size:** 530,536 bytes (~518 KB)
- **SHA-256:** `2e76a89a3f6e6a5fe6f924c7a5730c3a93ef0d2a9c68b7d9fdd7d984675d0d4a`
- **Header bytes:** `0d00 0000 2e00 0000 f3a0 0500` →
  - SaveHeaderVersion = **13**
  - SaveVersion = **46** (1.0 era)
  - BuildVersion = **368883** (Satisfactory 1.0 release build, Sept 2024)
- **Session name:** `Release`
- **Playtime:** 6h00m44s (early game)
- **Saved at:** 2024-09-29
- **Map:** `Persistent_Level`, single-player session

Header v13 layout note: fields are `headerVersion, saveVersion, buildVersion,
mapName, mapOptions, sessionName, playDurationSeconds (int32), saveDateTime (int64 FDateTime ticks), ...`

## megafactory.sav

- **Original filename:** `THP 10.0.sav`
- **Source:** https://raw.githubusercontent.com/nusje2000/satisfactory-saves/main/ee750ca5bb024a1281b951cd4ff53753/THP%2010.0.sav
  (public personal save-backup repo [nusje2000/satisfactory-saves](https://github.com/nusje2000/satisfactory-saves), no license file —
  publicly shared save data, used here as a test fixture only)
- **File size:** 23,239,182 bytes (~22.2 MB)
- **SHA-256:** `79d2e8ead045d3150f60dab1d7e43787f6d364f3c3c04acf914d92bfbea56b61`
- **Header bytes:** `0e00 0000 3400 0000 b410 0700` →
  - SaveHeaderVersion = **14**
  - SaveVersion = **52** (1.1 era)
  - BuildVersion = **463028**
- **Save name:** `THP 10.0`
- **Session name:** `Leaking Blood Vessel`
- **Playtime:** 652h37m11s (genuine megafactory)
- **Saved at:** 2025-12-26
- **Map:** `Persistent_Level`

Header v14 layout note: v14 inserts the save name string between `buildVersion` and
`mapName`: `headerVersion, saveVersion, buildVersion, saveName, mapName, mapOptions,
sessionName, playDurationSeconds, saveDateTime, ...`

## current_1_2.sav

- **Original filename:** `Another-1-2.sav`
- **Source:** https://raw.githubusercontent.com/etothepii4/satisfactory-file-parser/main/src/test/Another-1-2.sav
  (test fixture in [etothepii4/satisfactory-file-parser](https://github.com/etothepii4/satisfactory-file-parser), MIT licensed)
- **File size:** 139,551 bytes (~136 KB)
- **SHA-256:** `3defce5b2093aef5035811f6d8fed1ba4ab581dcbe85c926f560ba84b479d5f5`
- **Header bytes:** `0e00 0000 3a00 0000 2c5a 0700` →
  - SaveHeaderVersion = **14**
  - SaveVersion = **58** (1.2 era, current)
  - BuildVersion = **481836**
- **Session name:** `Another 1.2 Baby`

## current_sv60.sav

- **Original filename:** `Josh 2.sav`
- **Source:** Josh's gnomon Satisfactory install (real save, captured 2026-06-15
  during the first end-to-end daemon push). First sv60 save we had — the
  v1.0.0 ceiling (58) rejected it, prompting verification + the bump to 60.
- **File size:** 334,577 bytes (~327 KB)
- **SHA-256:** `6f4b5dfaf083ea28b93ba317601f5437cd8df33465107a81fa7bdb906f41432e`
- **SaveHeaderVersion = 14, SaveVersion = 60, BuildVersion = 493833**
- **Session name:** `Josh` — Tier 3, 5.3 hours played

## Coverage

The four fixtures cover both modern header versions (13 and 14) and save
versions 46 (= 1.0), 52 (= 1.1), 58 (= early 1.2), and 60 (= later 1.2 patch).

`megafactory.sav` is gitignored (22 MB) — re-download from the source URL above
if missing; tests that need it skip when absent.

## Rejected candidates

- `FreshStart001.sav` / `FreshStart002.sav` (etothepii4) — SaveVersion 41 / header v10, pre-1.0.
- `maksimoancha/satisfactory-base-save` (SuperMegaBase) — Update 3 era, far pre-1.0.
- `enilsson18/SatisfactorySaves`, `windings-lab/SatisfactorySaves` — pre-1.0 (pushed 2021/2022).
