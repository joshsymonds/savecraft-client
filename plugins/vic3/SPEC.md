# Victoria 3 Plugin Spec

## Overview

A Savecraft plugin that parses Victoria 3 save files and exposes structured game state to AI companions via MCP. Built in Rust using [jomini](https://github.com/rakaly/jomini) for Clausewitz format parsing, compiled to WASI.

The AI companion use case is strongest here: Vic3 is notoriously complex and opaque. The companion reads the player's actual game state and explains what's happening, why, and what to do — essentially teaching them to play through their own save.

## Architecture

### Why Rust

- **jomini** is the gold-standard Clausewitz parser (handles EU4, CK3, HOI4, Vic3, Imperator, EU5)
- Rust compiles to `wasm32-wasip1` — same WASI target as Go plugins
- Eliminates the riskiest component (writing a bespoke Clausewitz parser)
- jomini already handles: text format, binary format, ZIP envelopes, cross-game quirks
- First non-Go plugin — validates that the plugin contract is truly language-agnostic

### Plugin Contract (unchanged)

- **stdin**: raw `.v3` file bytes (ZIP containing `gamestate` + `meta`)
- **stdout**: ndjson (`status`, `result`, `error` lines)
- **Result line**: `{ identity, summary, sections }` — same as all plugins
- **2MB cap** on the result line — requires aggressive summarization for late-game saves

### Two WASM Binaries

| Binary | Runs in | Purpose |
|--------|---------|---------|
| `parser.wasm` | Daemon (local) | Parses `.v3` saves → sections |
| `reference.wasm` | Worker (cloud) | Queries game data (buildings, laws, techs, goods) |

Same split as D2R.

### Repo Layout

Rust crates live alongside Go code in the monorepo. A root-level Cargo workspace ties them together; each game plugin stays in `plugins/` so the existing `just build-plugin`, `just sign-plugins`, and `just build-plugins` work unchanged.

```
savecraft.gg/
├── Cargo.toml                    # Workspace: members = ["libs/clausewitz-core", "plugins/vic3/parser", "plugins/vic3/reference"]
├── Cargo.lock
├── go.mod                        # Go module (coexists)
├── libs/
│   └── clausewitz-core/          # Shared Rust crate
│       ├── Cargo.toml
│       └── src/
│           └── lib.rs            # jomini integration, ndjson output, ZIP envelope, stdin/stdout
├── plugins/
│   ├── d2r/                      # Go plugin — unchanged
│   │   ├── Justfile
│   │   ├── plugin.toml
│   │   ├── parser/
│   │   └── reference/
│   └── vic3/                     # Rust plugin — same Justfile interface
│       ├── Justfile              # build, test, build-parser, build-reference
│       ├── plugin.toml
│       ├── SPEC.md
│       ├── parser/
│       │   ├── Cargo.toml        # [[bin]] vic3-parser, depends on clausewitz-core
│       │   └── src/
│       │       ├── main.rs       # Entry point: stdin → jomini → sections → ndjson stdout
│       │       └── sections/     # Per-section extraction (overview.rs, economy.rs, etc.)
│       └── reference/
│           ├── Cargo.toml        # [[bin]] vic3-reference, depends on clausewitz-core
│           └── src/
│               ├── main.rs       # Query dispatch: stdin JSON → lookup → stdout JSON
│               └── data/         # Embedded game data (generated from common/ files)
└── ...
```

### Build System

Each plugin exposes the same Justfile interface (`build`, `test`, `build-parser`, `build-reference`) regardless of language. The root Justfile delegates via `cd plugins/{{name}} && just build` — it doesn't need to know whether the plugin is Go or Rust.

```just
# plugins/vic3/Justfile
build: build-parser build-reference

build-parser:
    cargo build -p vic3-parser --target wasm32-wasip1 --release
    cp ../../target/wasm32-wasip1/release/vic3-parser.wasm parser.wasm

build-reference:
    cargo build -p vic3-reference --target wasm32-wasip1 --release
    cp ../../target/wasm32-wasip1/release/vic3-reference.wasm reference.wasm
    cp reference.wasm ../../reference/

test:
    cargo test -p vic3-parser -p vic3-reference
```

The root `sign-plugins` glob (`plugins/*/*.wasm`) finds the copied `.wasm` files in `plugins/vic3/` as expected.

## Save Format

Victoria 3 saves are ZIP archives containing two Clausewitz-format files:

- **`meta`** — lightweight metadata (date, player country, DLC flags, ironman status)
- **`gamestate`** — the full game state (economy, pops, politics, military, diplomacy, etc.)

Both can be text or binary encoded. Default is `zip_binary_all`; players can switch to text via `pdx_settings.json` but this is **not required** — the parser handles both formats.

jomini handles both encodings transparently. Binary saves use token IDs instead of string keys (e.g., `0x2843` instead of `"country_manager"`); jomini resolves these via a game-version-specific token mapping that ships alongside the WASM binary. This mapping is available from the [rakaly](https://github.com/rakaly) ecosystem.

### Clausewitz Format

Nested key-value pairs with braces. No formal spec — each game has quirks. Example:

```
date = 1856.3.15
player = "GBR"
country_manager = {
    database = {
        1 = {
            definition = "GBR"
            budget = { ... }
            technology = { ... }
        }
    }
}
```

jomini abstracts the parsing. Our work is walking the resulting tree and extracting meaningful sections.

## Section Design

Sections are designed around player questions, not data model topology. A confused Vic3 player should be able to ask natural questions and the companion should find the answer in the relevant section.

### Sections (MVP — 5 sections)

#### `overview`
**Player question:** "How is my country doing?"

```json
{
  "country": "Great Britain",
  "tag": "GBR",
  "date": "1856-03-15",
  "rank": 1,
  "prestige": 1247,
  "gdp": 142000,
  "gdp_trend": "+3.2%",
  "treasury": 28400,
  "income": 4200,
  "expenses": 3800,
  "infamy": 12.4,
  "population": 24800000,
  "literacy": 0.42,
  "avg_sol": 12.8
}
```

**Identity**: `{ saveName: filename, gameId: "vic3", extra: { tag: "GBR", date: "1856-03-15" } }`
**Summary**: `"Great Britain, #1 Great Power (1856)"`

#### `economy`
**Player question:** "Why is my economy broken?" / "What should I build?"

```json
{
  "income_breakdown": {
    "taxes": 2800,
    "trade": 900,
    "other": 500
  },
  "expense_breakdown": {
    "military": 1200,
    "construction": 800,
    "government": 600,
    "subsidies": 400,
    "other": 800
  },
  "goods_shortages": [
    { "good": "iron", "demand": 340, "supply": 180, "price_modifier": 1.42, "import": 160 },
    { "good": "tools", "demand": 280, "supply": 220, "price_modifier": 1.18, "import": 60 }
  ],
  "goods_surpluses": [
    { "good": "grain", "demand": 400, "supply": 520, "price_modifier": 0.78, "export": 120 }
  ],
  "construction": {
    "weekly_points": 120,
    "queue_size": 8,
    "queue": ["iron_mine (Silesia)", "textile_mill (Lancashire)", "..."]
  },
  "top_buildings_by_profit": [
    { "building": "Coal Mine", "state": "Wales", "level": 8, "profit": 420 }
  ],
  "top_buildings_by_loss": [
    { "building": "Arms Industry", "state": "London", "level": 3, "profit": -180 }
  ]
}
```

Aggressive summarization: top 5 shortages/surpluses, top 5 profitable/unprofitable buildings. Full lists would blow the 2MB cap in a large empire.

#### `population`
**Player question:** "Are my people happy?" / "Why do I have so many radicals?"

```json
{
  "total": 24800000,
  "avg_standard_of_living": 12.8,
  "sol_trend": "+0.3",
  "loyalists": 4200000,
  "radicals": 1800000,
  "literacy": 0.42,
  "pop_by_strata": {
    "upper": 320000,
    "middle": 3800000,
    "lower": 20680000
  },
  "top_cultures": [
    { "culture": "British", "population": 18200000, "acceptance": "primary" },
    { "culture": "Irish", "population": 4800000, "acceptance": "discriminated" }
  ],
  "states_by_turmoil": [
    { "state": "Ireland", "radicals_pct": 0.34, "sol": 8.2 }
  ]
}
```

#### `politics`
**Player question:** "What laws should I pass?" / "Why can't I enact this reform?"

```json
{
  "government_type": "Parliamentary Republic",
  "legitimacy": 72,
  "active_laws": [
    { "category": "economic_system", "law": "Laissez-Faire" },
    { "category": "labor_rights", "law": "No Workers Rights" }
  ],
  "interest_groups": [
    {
      "name": "Industrialists",
      "leader": "Lord Palmerston",
      "clout": 0.28,
      "approval": "happy",
      "in_government": true,
      "ideology": "liberal"
    }
  ],
  "active_movements": [
    { "movement": "Enact Trade Unions", "support": 340000, "radicalism": 0.4 }
  ],
  "active_institutions": [
    { "name": "Colonial Affairs", "level": 3, "investment": 200 }
  ]
}
```

#### `military`
**Player question:** "Am I about to get invaded?" / "Can I win this war?"

```json
{
  "army_size": 120000,
  "navy_size": 84,
  "manpower": 42000,
  "generals": [
    { "name": "Wellington", "rank": 3, "army_size": 40000, "location": "Punjab" }
  ],
  "active_wars": [
    {
      "name": "Sikh War",
      "our_side": "attacker",
      "war_score": 62,
      "enemies": ["Punjab"],
      "allies": []
    }
  ],
  "diplomatic_plays": [],
  "conscription_potential": 180000
}
```

### Sections (Post-MVP)

#### `diplomacy`
Relations, alliances, subjects, customs unions, active diplomatic plays.

#### `technology`
Current era, researched techs grouped by category, available techs, innovation rate.

#### `trade`
Trade routes by good, import/export partners, tariff revenue.

## Reference Module: Game Rules Database

Unlike D2R's drop calculator (which computes probabilities), Vic3's reference module is a **game rules lookup**. It answers:

- "What does building X produce/consume?"
- "What production methods are available for X?"
- "What does law X do? What modifiers does it apply?"
- "What does tech X unlock?"
- "What are the inputs for goods chain X?"

### Data Source

Vic3's game data lives in `game/common/` as Clausewitz-format text files:

```
game/common/
├── buildings/          # Building definitions (inputs, outputs, levels)
├── production_methods/ # PM definitions (what each method changes)
├── goods/              # Goods definitions (category, cost weight)
├── technology/         # Tech tree (era, unlocks, category)
├── laws/               # Law definitions (effects, modifiers)
├── interest_groups/    # IG definitions (ideologies, traits)
├── modifiers/          # Modifier definitions
└── ...
```

These are parseable by jomini — same format as save files. No CASC extraction needed; they're plain files in the game install directory.

### Data Pipeline

1. Copy relevant `game/common/` directories from Vic3 install
2. Parse with jomini → extract into typed Rust structs
3. Serialize as embedded data (compile-time constants or generated code)
4. At runtime: query by name/category, return structured JSON

### Reference Queries

```json
// "What does an iron mine produce?"
{ "module": "game_rules", "type": "building", "name": "iron_mine" }

// "What does Laissez-Faire do?"
{ "module": "game_rules", "type": "law", "name": "laissez_faire" }

// "What does Railways unlock?"
{ "module": "game_rules", "type": "technology", "name": "railways" }

// "What goods are in the industrial category?"
{ "module": "game_rules", "type": "goods", "category": "industrial" }
```

## Reusability: Clausewitz Across Games

The `clausewitz-core` crate in `libs/clausewitz-core/` provides the shared parsing layer from day one. Adding a second Paradox game plugin (e.g., CK3) means:

1. Create `plugins/ck3/parser/` and `plugins/ck3/reference/` crates
2. Depend on `clausewitz-core` for jomini integration, ndjson output, ZIP handling
3. Implement game-specific section extraction and reference data
4. Add `plugins/ck3/Justfile` with the standard `build`/`test` targets

**Shared** (in `clausewitz-core`): Clausewitz parser, WASI stdin/stdout, ndjson serialization, ZIP envelope, binary token resolution.

**Game-specific** (in each plugin): Section definitions, summarization logic, reference data, game knowledge, token mappings.

## Estimates

| Component | LOC | Notes |
|-----------|-----|-------|
| Rust scaffolding + ndjson | 200-300 | stdin/stdout, JSON serialization |
| jomini integration | 300-500 | ZIP handling, tree walking, binary token resolution |
| Section extraction (5 MVP) | 1,500-2,000 | Tree → typed structs → summarized JSON |
| Reference module | 800-1,200 | Game data parsing + query dispatch |
| Game data embedding | 500-1,000 | Generated or embedded from `common/` files |
| Tests + fixtures | 600-1,000 | Need real save fixtures (text mode) |
| Build tooling | 100-200 | Justfile targets, CI |
| **Total MVP** | **~4,000-6,200** | |

## Open Questions

1. **Token mapping versioning** — Binary saves use token IDs resolved via a game-version-specific mapping from the rakaly ecosystem. Need to determine: bundle a single mapping per plugin release, or support multiple game versions? Vic3 patches may shift token IDs.

2. **Save size vs 2MB cap** — Late-game saves with 100+ countries can be 30-50MB of text. Summarization strategy needs testing with real saves. May need configurable detail levels.

3. **Game version coupling** — Vic3 patches change data formats. How tightly coupled is the parser to a specific game version? jomini abstracts the format; the risk is in section extraction assumptions.

4. **Ironman saves** — Ironman saves are binary-only and checksum-protected. jomini can parse them but they can't be converted to text. Needs testing.

5. **Shared Clausewitz crate** — Extract common code if/when a second Paradox game plugin is built. Not worth abstracting prematurely for just Vic3.
