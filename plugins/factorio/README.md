# Factorio Plugin

Savecraft integration for Factorio (base game + Space Age expansion).

## Architecture

Factorio uses a **hybrid mod + daemon** approach — unlike any other Savecraft plugin:

- **Lua mod** runs inside Factorio, collects game state via the Lua API, and writes structured JSON to `script-output/savecraft/` using `helpers.write_file()`
- **Savecraft daemon** watches the `script-output/savecraft/` directory via fsnotify, reads the JSON, and pushes it to the server as save data
- **WASM parser plugin** is a thin pass-through (the mod already outputs structured JSON, so no binary parsing needed)
- **Go WASM reference modules** provide deterministic calculators with baked-in game data

This is necessary because:
1. Factorio's binary save format (`level.dat`) is undocumented, changes every patch, and no external parser has successfully decoded the world state — only metadata headers
2. Factorio's mod sandbox blocks network access (no WebSocket push like RimWorld), but allows file writes to a single known directory
3. The Lua API has deep read access to virtually ALL game state — production stats, research, inventories, logistics, trains, power, combat, fluids, Space Age surfaces

### Why Not Binary Save Parsing?

Factorio developer Rseding91: *"We don't have a fixed-size format — everything is serialized. So you have to know what a piece of data means before you can say how many bytes you should read. The file format changes with each update."*

No one has built a working `level.dat` parser beyond metadata extraction. The mod-based approach is version-proof and has access to richer data (e.g., `LuaFlowStatistics` rate data that isn't in the save file at all).

### Why Not a Direct WebSocket Mod (RimWorld Pattern)?

Factorio's Lua sandbox strips `io.*`, `socket`, `os.execute`, and all network libraries. Mods can only write files, not open connections. The daemon bridges the gap.

### Precedent

This pattern is well-established in the Factorio ecosystem:
- **Graftorio2** — mod writes stats to `script-output/`, sidecar reads and pushes to Prometheus/Grafana
- **FDAT** — mod exports production statistics as JSON for external dashboards
- **Statorio** — real-time JSON export of configurable game metrics

## Lua Mod

The mod runs in Factorio's control stage (`control.lua`) and hooks `script.on_nth_tick()` to periodically dump game state.

### Key APIs Used

| API | Purpose |
|-----|---------|
| `helpers.write_file(path, data, append)` | Write JSON to `script-output/savecraft/` |
| `game.table_to_json(table)` | C++-backed JSON serializer (fast, handles large tables) |
| `script.on_nth_tick(N, func)` | Periodic state dump (e.g., every 300 ticks = 5 seconds) |
| `force.item_production_statistics` | Per-item production/consumption counts and rates |
| `force.fluid_production_statistics` | Per-fluid production/consumption counts and rates |
| `force.technologies` | Research tree state (completed, in progress, queued) |
| `force.evolution_factor` | Biter evolution with source breakdown |
| `player.get_inventory(defines.inventory.*)` | Player inventory contents |
| `surface.find_entities_filtered(...)` | Entity queries (drills, turrets, roboports, etc.) |
| `game.connected_players` | Active player list |
| `entity.electric_network_statistics` | Power generation/consumption |

### File Output

The mod writes to `script-output/savecraft/state.json` (overwrite mode). The daemon watches this file.

Output location by platform:
- **Linux:** `~/.factorio/script-output/savecraft/`
- **Windows:** `%APPDATA%/Factorio/script-output/savecraft/`
- **macOS:** `~/Library/Application Support/factorio/script-output/savecraft/`

### Mod Distribution

Published on the [Factorio Mod Portal](https://mods.factorio.com/). Players install via the in-game mod browser (no manual file management). The mod does NOT disable achievements (only `/c` console commands do).

## Save Data Sections

These represent **what the player has** — current game state exported by the mod. Each section is a JSON object stored in D1 and fetched via `get_section`. Since Savecraft ships deltas for updated sections and gzips first, we can be generous with data volume.

### `game_overview`

Map identity and high-level game state.

```json
{
  "map_name": "My Factory",
  "game_version": "2.0.28",
  "ticks_played": 3600000,
  "hours_played": 16.67,
  "difficulty": "normal",
  "mods": [{"name": "base", "version": "2.0.28"}],
  "rocket_launches": 3,
  "surfaces": ["nauvis", "vulcanus", "fulgora"],
  "active_surface": "nauvis"
}
```

### `production_flow`

Per-item and per-fluid production/consumption rates. Uses `LuaFlowStatistics` precision indices for per-minute rates. This is the critical section for bottleneck detection ("you need 2.3x more red circuits").

```json
{
  "items": {
    "iron-plate": {
      "produced_total": 1500000,
      "consumed_total": 1420000,
      "produced_per_min": 450.0,
      "consumed_per_min": 420.0
    },
    "electronic-circuit": {
      "produced_total": 800000,
      "consumed_total": 790000,
      "produced_per_min": 200.0,
      "consumed_per_min": 195.0
    }
  },
  "fluids": {
    "petroleum-gas": {
      "produced_total": 5000000,
      "consumed_total": 4800000,
      "produced_per_min": 1200.0,
      "consumed_per_min": 1100.0
    }
  },
  "top_deficits": ["copper-plate", "steel-plate", "plastic-bar"],
  "top_surpluses": ["stone", "coal"]
}
```

### `machines`

What's placed in the world and what it's doing. Grouped by active recipe — machine type, count, and module configuration. Enables the AI to say "you have 20 AM2s making green circuits with no modules, add productivity modules."

```json
{
  "by_recipe": {
    "electronic-circuit": {
      "machine_type": "assembling-machine-2",
      "count": 20,
      "modules": {"speed-module": 15, "speed-module-2": 10, "none": 15}
    },
    "advanced-circuit": {
      "machine_type": "assembling-machine-2",
      "count": 8,
      "modules": {"productivity-module": 16}
    }
  },
  "by_type": {
    "assembling-machine-1": 15,
    "assembling-machine-2": 45,
    "assembling-machine-3": 20,
    "chemical-plant": 12,
    "oil-refinery": 8,
    "electric-furnace": 40,
    "steel-furnace": 30
  },
  "beacon_count": 24
}
```

### `research`

Current research progress, queue, completed technologies, and infinite research levels.

```json
{
  "current": {
    "name": "chemical-science-pack",
    "progress": 0.45,
    "cost_per_unit": 75,
    "unit_count": 200,
    "ingredients": [["automation-science-pack", 1], ["logistic-science-pack", 1]]
  },
  "queue": ["advanced-oil-processing", "robotics"],
  "completed": ["automation", "logistics", "electronics", "steel-processing"],
  "completed_count": 42,
  "total_available": 200,
  "infinite_levels": {
    "mining-productivity": 5,
    "artillery-shell-range": 2,
    "worker-robots-speed": 3
  }
}
```

### `resources`

Resource patches that have mining drills on them. Center position, remaining amount, drill count, extraction rate. Plus global mining productivity bonus.

```json
{
  "patches": [
    {
      "type": "iron-ore",
      "surface": "nauvis",
      "center": {"x": 120, "y": -45},
      "remaining": 2500000,
      "initial_estimate": 5000000,
      "tiles": 150,
      "drills": 28,
      "extraction_rate_per_min": 840
    }
  ],
  "mining_productivity_bonus": 0.5
}
```

### `power`

Per-surface power generation, consumption, and satisfaction. Generator breakdown by type.

```json
{
  "surfaces": {
    "nauvis": {
      "generation_mw": 45.2,
      "consumption_mw": 38.7,
      "satisfaction": 1.0,
      "generators": {
        "steam-engine": {"count": 40, "mw": 36.0},
        "solar-panel": {"count": 50, "mw": 2.1},
        "nuclear-reactor": {"count": 0, "mw": 0.0}
      },
      "accumulators": {"count": 42, "charge_mj": 210.0, "capacity_mj": 210.0}
    }
  }
}
```

### `fluids`

Oil processing setup, fluid tank levels, and fluid-specific production data. Oil is complex enough to warrant its own section.

```json
{
  "refineries": {
    "advanced-oil-processing": 8,
    "basic-oil-processing": 0,
    "coal-liquefaction": 0
  },
  "chemical_plants": {
    "heavy-oil-cracking": 3,
    "light-oil-cracking": 5,
    "sulfur": 2,
    "plastic-bar": 4,
    "sulfuric-acid": 1
  },
  "tank_levels": {
    "crude-oil": {"current": 15000, "capacity": 25000},
    "heavy-oil": {"current": 200, "capacity": 25000},
    "light-oil": {"current": 8000, "capacity": 25000},
    "petroleum-gas": {"current": 20000, "capacity": 25000},
    "lubricant": {"current": 5000, "capacity": 25000},
    "sulfuric-acid": {"current": 12000, "capacity": 25000}
  }
}
```

### `logistics`

Per-surface roboport coverage, bot counts, and logistics network state.

```json
{
  "surfaces": {
    "nauvis": {
      "roboports": 12,
      "logistic_bots": {"total": 150, "available": 45},
      "construction_bots": {"total": 80, "available": 30},
      "construction_queue": 15,
      "logistic_chests": {
        "passive-provider": 200,
        "requester": 50,
        "storage": 30,
        "buffer": 10,
        "active-provider": 5
      }
    }
  }
}
```

### `trains`

Train list with composition, schedule, cargo, and fuel. Station list with throughput data.

```json
{
  "trains": [
    {
      "id": 1,
      "composition": "1-4",
      "state": "on_the_path",
      "schedule": [
        {"station": "Iron Mine", "wait": "full cargo"},
        {"station": "Iron Smelter", "wait": "empty cargo"}
      ],
      "cargo": {"iron-ore": 8000},
      "fuel": "solid-fuel"
    }
  ],
  "stations": [
    {"name": "Iron Mine", "position": {"x": 500, "y": -200}, "train_limit": 2}
  ]
}
```

### `defenses`

Evolution factor with source breakdown, turret and wall counts, nearby enemy bases, recent attack frequency.

```json
{
  "evolution": {
    "factor": 0.45,
    "time_factor": 0.15,
    "pollution_factor": 0.25,
    "kill_factor": 0.05
  },
  "turrets": {
    "gun-turret": 50,
    "laser-turret": 20,
    "flamethrower-turret": 8,
    "artillery-turret": 2
  },
  "walls": 500,
  "enemy_bases_nearby": [
    {"distance": 120, "direction": "northeast", "size": "medium"}
  ],
  "recent_attacks": 3,
  "pollution_cloud_radius_chunks": 12
}
```

### `inventory`

Player inventory, equipment, crafting queue, and position.

```json
{
  "player": {
    "main": {"iron-plate": 50, "copper-plate": 30, "rail": 200},
    "armor": "power-armor-mk2",
    "equipment_grid": ["personal-roboport-mk2", "fusion-reactor", "exoskeleton"],
    "crafting_queue": [{"recipe": "rail", "count": 50}],
    "position": {"x": 0, "y": 0, "surface": "nauvis"}
  }
}
```

### `surfaces`

Per-surface summary for Space Age. Planet type, entity counts, pollution. For platforms: route, cargo, thrusters.

```json
{
  "surfaces": [
    {"name": "nauvis", "type": "planet", "entities": 5000, "chunks_charted": 200, "pollution_total": 12000},
    {"name": "vulcanus", "type": "planet", "entities": 200, "chunks_charted": 30, "pollution_total": 0},
    {"name": "platform-1", "type": "platform", "entities": 50, "route": "nauvis > vulcanus", "thrusters": 8, "asteroid_collectors": 4}
  ]
}
```

### `alerts`

Active game alerts — high-signal ephemeral data the AI can flag immediately.

```json
{
  "no_fuel": 2,
  "no_power": 5,
  "no_storage": 1,
  "turret_ammo_low": 3,
  "train_no_path": 1,
  "damaged_entities": 8
}
```

### `blueprint_library`

Index only — names, types, and entity counts. Full blueprint strings are too large; players paste strings directly for analysis.

```json
{
  "blueprints": [
    {"name": "4-lane balancer", "type": "blueprint", "entity_count": 32},
    {"name": "Oil Processing", "type": "blueprint-book", "children": [
      {"name": "Basic Oil", "type": "blueprint", "entity_count": 15},
      {"name": "Advanced Oil + Cracking", "type": "blueprint", "entity_count": 48}
    ]}
  ]
}
```

## Reference Modules

Reference modules are **deterministic calculators with baked-in game data**. They prevent the AI from hallucinating recipe counts, ratios, and formulas. All modules compile into a single Go WASM binary (`reference.wasm`), following the RimWorld pattern.

### Data Pipeline

```
factorio --dump-data
    -> script-output/data-raw-dump.json (all prototype definitions)
    -> Go datagen tool parses + extracts recipes, items, entities, technologies
    -> Generated Go struct literals (recipes_gen.go, items_gen.go, techs_gen.go)
    -> Compiled into reference.wasm (GOOS=wasip1 GOARCH=wasm)
```

Data updates when Factorio patches (every few months). The prototype dump includes all recipes, items, entities, and technologies with exact numbers — craft times, ingredient counts, energy consumption, module slots, etc.

### Anti-Hallucination Modules

#### `recipe_lookup`

*Analogous to MTGA `card_search` and RimWorld `materials`*

Look up any item, recipe, entity, or technology by exact name. Supports reverse lookups ("what uses copper cable?", "what produces plastic?").

- **Why:** Factorio has 400+ recipes. The AI will hallucinate ingredient counts and craft times. "Electronic circuit needs 1 iron plate and 3 copper cable" — the ingredient count is right but each cable craft produces 2, so the ratio math depends on knowing this.
- **Input:** `name`, `type` (item/recipe/entity/technology), `usage` (items that consume this), `product` (recipes that produce this)
- **Output:** Exact recipe data — ingredients with counts, products with counts, craft time, machine category, allowed modules, energy consumption
- **Prevents:** Wrong ingredient counts, wrong craft times, wrong machine types, made-up recipes

#### `tech_tree_navigator`

*Analogous to RimWorld `research`*

Full prerequisite chains with total science pack costs, optimal research paths.

- **Why:** Tech tree is a graph with 200+ nodes. "What do I need to unlock spidertron?" requires traversal the AI shouldn't do in-context.
- **Input:** `target` technology, `completed` list (from save via section mapping), `goal` mode (shortest path)
- **Output:** Full prerequisite chain, total cost by science pack type, recommended research order
- **Section mapping:** Pulls `research` section to exclude already-completed technologies

### Calculator Modules

#### `ratio_calculator`

*The killer module — no direct analogue in other Savecraft plugins*

Given a target item and production rate, compute the full dependency tree: machine counts by tier, belt lane requirements, inserter types, raw material input rates.

- **Why:** This is THE thing players struggle with. The math involves `effective_speed = base_speed * (1 + sum(module_speed) + sum(beacon_effects))` where `beacon_effect = module_effect * distribution_efficiency / sqrt(n)`. Plus productivity bonuses create "free" items that change the ratios for all downstream consumers. The AI WILL get this wrong.
- **Input:** `target_item`, `target_rate` (items/min), `assembler_tier`, `modules` (optional), `beacon_count` (optional), `beacon_modules` (optional)
- **Output:** Full dependency tree — machine counts per intermediate, belt tier requirements, raw material rates, total power consumption, total pollution. Factor breakdown showing module/beacon contributions at each stage.
- **Section mapping:** Pull `production_flow` to compute deltas ("you produce X, you need Y, here's the gap"). Pull `machines` to see current module configs and suggest upgrades.

#### `oil_balancer`

*Specialized ratio calculator for the fluid system*

Given target fluid products and rates, compute optimal refinery + cracking plant counts.

- **Why:** Oil processing is notoriously confusing. Advanced oil processing produces three outputs simultaneously; cracking ratios depend on what you actually need; players frequently over-crack or under-crack. The optimal ratio for all-petroleum is 8:2:7 (refineries : heavy cracking : light cracking) — not intuitive.
- **Input:** `processing_type` (basic/advanced/coal_liquefaction), `targets` (petroleum, lubricant, solid_fuel rates), `modules` (optional)
- **Output:** Refinery count, heavy cracking plant count, light cracking plant count, water input rate, crude oil input rate, surplus byproducts, total power consumption
- **Section mapping:** Pull `fluids` section to see current setup and compute what to change

#### `power_calculator`

*Analogous to RimWorld power analysis but as an interactive calculator*

Given power demand, compute optimal generation setup.

- **Why:** Nuclear neighbor bonus formula (each adjacent fueled reactor adds 100% to base output), solar/accumulator ratio (25:21 for continuous power), and steam engine ratios (1 offshore pump : 20 boilers : 40 steam engines) are specific numbers players shouldn't guess.
- **Input:** `target_mw`, `type` (steam/solar/nuclear), `reactor_layout` (optional, e.g., "2x4")
- **Output:** Exact entity counts + fuel consumption. For nuclear: reactors, heat exchangers, turbines, fuel cells/minute, uranium ore/minute. For solar: panels, accumulators. For steam: boilers, engines, offshore pumps. Includes power surplus calculation.
- **Section mapping:** Pull `power` section to compute current deficit

#### `blueprint_analyzer`

*Unique to Factorio — no analogue in other plugins*

Decode a blueprint string and evaluate its efficiency against recipe data.

- **Why:** Blueprint strings are `version_byte + base64(zlib(json))`. The AI literally cannot decode them in-context. This module decodes, extracts the entity list, cross-references with baked-in recipe data, and evaluates production ratios.
- **Input:** `blueprint_string` (pasted by user)
- **Output:** Entity list with positions, production ratios vs optimal (is the green-to-red circuit ratio correct?), belt throughput analysis (can the belts handle the production rate?), module audit (are machines using appropriate modules?), inserter adequacy (can inserters keep up?), compactness score, improvement suggestions
- **No section mapping** — user pastes blueprint strings directly into chat

#### `evolution_tracker`

*Analogous to RimWorld `raids`*

Compute evolution factor and predict enemy tier thresholds.

- **Why:** Evolution uses asymptotic squashing (`evolution = raw / (1 + raw)`) with `(1 - evolution)^2` marginal scaling. Players can't intuit when behemoths will appear or whether pollution or nest-clearing is the dominant driver.
- **Input:** `game_time_hours`, `pollution_absorbed`, `nests_destroyed` (or pull from save)
- **Output:** Current evolution factor, next enemy tier + threshold, dominant evolution source, estimated time/pollution to next tier at current rates
- **Section mapping:** Pull `defenses` section for evolution data and pollution rate

#### `module_optimizer`

*Analogous to optimization in RimWorld crop calculator*

Given a production target, recommend optimal module/beacon configuration.

- **Why:** The optimization space is large — 3 module types * 3 tiers * up to 4 slots * variable beacon counts with sqrt(n) diminishing returns, all interacting with productivity bonuses. Players can't intuit the optimum.
- **Input:** `machine_type`, `recipe`, `target_rate`, `available_module_tiers`, `max_beacons`, `constraints` (max power draw, max pollution)
- **Output:** Recommended configuration, resulting effective speed, power consumption, pollution, productivity bonus, items/min. Comparison table of key alternatives (e.g., "4x prod3 + 8 beacons vs 4x speed3 + no beacons").

#### `quality_calculator`

*Space Age specific — no analogue*

Compute quality tier probability distributions and recycler loop efficiency.

- **Why:** Quality involves repeated geometric probability rolls (succeed at X%, then roll again at 10% for the next tier). Recycler loops lose 75% of materials per cycle. "How many normal items to craft one legendary?" is deeply unintuitive math.
- **Input:** `quality_module_tier`, `module_count`, `machine_quality_tier`, `recycler_loop` (bool), `input_quality_tier`
- **Output:** Probability distribution per quality tier, expected input materials per legendary output, optimal recycler loop configuration, material efficiency comparison

#### `train_throughput`

*Calculator for train logistics planning*

Compute train network throughput between stations.

- **Why:** Throughput depends on train length, fuel acceleration bonuses, round-trip distance, inserter loading/unloading speed, and signal block spacing. Players consistently misjudge capacity.
- **Input:** `locomotive_count`, `wagon_count`, `fuel_type`, `distance_tiles`, `inserter_type`, `inserters_per_wagon`, `signal_block_spacing`
- **Output:** Round-trip time breakdown (loading + acceleration + travel + braking + unloading), items/min throughput, bottleneck identification (loading, travel, or signal blocks), capacity headroom

## Visualizations

Reference modules and save sections that benefit from visual presentation get companion Svelte view components, following the existing Savecraft view system (SVG + DOM in Svelte, bundled at build time, rendered in MCP Apps iframes).

### Icon System

Factorio item/recipe icons are extracted via `factorio --dump-icon-sprites` into a single PNG sprite sheet (~500 icons at 64x64, ~300KB total). Served from R2 with a JSON manifest mapping `item_name -> {x, y, w, h}`. CSS `background-image` + `background-position` renders individual icons from one HTTP request.

**Icon renderer is swappable.** The component system abstracts icon display behind a `<FactorioIcon name="iron-plate" />` component. Two rendering modes:

1. **Sprite mode** (default): CSS background-position into the sprite sheet. Shows actual game icons.
2. **Label mode** (fallback): Colored rectangle with item name text. Used if icons are unavailable or if commercial licensing requires it.

All views must work in label mode — icons are enhancement, not requirement. Tooltips on hover show item name, recipe, and module info regardless of icon mode.

### Chart Components

**Shared flow component** (in `views/src/components/charts/`, reusable across all games):

| Component | Purpose |
|-----------|---------|
| **FlowChart** | Sankey-style flow visualization — nodes are stages, bands are flows with width proportional to rate. Custom layout engine (BFS depth ordering, center-on-upstream positioning, overlap resolution). Game-agnostic: works for Factorio production chains, oil processing, RimWorld trade routes, any flow data. Pure Svelte (HTML divs for nodes, SVG paths for bands). |

Nodes accept a `nodeContent` snippet for game-specific rendering (icons, labels, badges). CSP-compliant, bundled at build time, no external layout library dependencies.

**New Factorio-specific component:**

| Component | Purpose |
|-----------|---------|
| **FactorioIcon** | Swappable icon renderer — sprite sheet mode (CSS background-position) or label mode (colored rectangle + text). Tooltip on hover shows item name + recipe + module info. |

**All other visualization uses the existing shared component library.** No Factorio-specific chart components beyond the icon renderer. The shared library already covers the needed patterns:

| Existing Component | Factorio Usage |
|-----------|---------|
| BarChart | `production_flow` deficits/surpluses, `power` generation breakdown |
| StackedBar | `power` generation mix, `fluids` processing ratios, `quality_calculator` tier distribution |
| ProgressRing | `evolution_tracker` factor toward next tier, `research` progress |
| ProgressBar | `resources` patch depletion percentage |
| Heatmap | `quality_calculator` distribution across tiers and module configs |
| DataTable | `recipe_lookup` results, `blueprint_analyzer` entity list |
| FactorChain | `ratio_calculator` module/beacon effect breakdown, `module_optimizer` comparison |
| Sparkline | `production_flow` rate trends over time |

### Modules Without Views

These modules return structured data for the LLM to reason over. No player-facing visualization — the AI narrates the results in conversation.

| Module | Why No View |
|--------|-------------|
| `recipe_lookup` | Anti-hallucination data for the model. Player doesn't need to see raw recipe tables — the AI cites the relevant facts. |
| `tech_tree_navigator` | Returns prerequisite chains for the AI to summarize. A FlowChart view could be added later but the text list is sufficient. |
| `module_optimizer` | Returns a recommendation + comparison table. AI presents the recommendation with reasoning. FactorChain in a future view if needed. |
| `train_throughput` | Returns throughput numbers + bottleneck identification. AI explains in context. |

### Modules With Views

These modules produce output the player benefits from SEEING — flow diagrams, health dashboards, distribution charts.

#### `ratio_calculator` — Production Chain Flow

The flagship visualization. Renders a left-to-right Sankey-style flow diagram via FlowChart:
- **Nodes** = recipes (machine icon + count + module indicators)
- **Bands** = item flows (width proportional to rate in items/min)
- **Highlights** = bottleneck nodes glow red, surplus nodes glow blue
- **Tooltip** = hover any node to see full recipe details, module effects, efficiency

Uses the shared FlowChart component with ProductionChain wrapper and MachineNode custom content.

When invoked with section mapping against `production_flow`, delta annotations show "+12 machines needed" or "current: 20, target: 32" per node.

#### `oil_balancer` — Fluid Flow Sankey

Sankey diagram showing fluid processing chain:
- **Nodes** = processing stages (crude oil, refineries, cracking plants, end products)
- **Links** = fluid flows with width proportional to rate (units/sec)
- **Color** = fluid type (black for crude, brown for heavy, yellow for light, cyan for petroleum)
- **Annotations** = machine counts at each stage, surplus/deficit indicators

FlowChart computes the layout; Svelte renders SVG paths with cubic bezier curves.

#### `blueprint_analyzer` — Blueprint Report

Not a spatial layout (that would require rendering every entity position). Instead:
- **DataTable** of entities with counts, grouped by function (production, logistics, power, defense)
- **BarChart** comparing actual ratios vs optimal ratios per recipe
- **FactorChain** showing belt throughput vs production rate per output
- **Summary badges** for overall score (ratio efficiency, belt adequacy, module usage, compactness)

#### `power_calculator` — Power Layout

- **StackedBar** showing generation mix (steam, solar, nuclear proportions)
- For nuclear: schematic grid showing reactor layout with neighbor bonus annotations
- **FactorChain** showing fuel consumption chain (uranium ore → fuel cells → heat → steam → electricity)

#### `evolution_tracker` — Threat Timeline

- **ProgressRing** showing current evolution toward next enemy tier
- **StackedBar** showing evolution source breakdown (time, pollution, kills)
- **Timeline** showing enemy tier unlock thresholds with current position marked

#### `quality_calculator` — Quality Distribution

- **Heatmap** showing probability distribution across quality tiers for different module configs
- **BarChart** comparing material cost per legendary item across recycler loop vs straight crafting

#### `production_flow` (save section) — Factory Health Dashboard

- **BarChart** of top deficits (items consumed faster than produced) in red, top surpluses in blue
- **Sparkline** trends if historical data is available
- **Badges** for critical alerts (items at zero production, severe bottlenecks)

### Design Principles

1. **Views are for the player, not the LLM.** Reference modules that exist to prevent AI hallucination (like `recipe_lookup`) return structured data for the model to reason over — no view needed. Views are only built for modules whose output the player needs to SEE: production chain diagrams, flow charts, health dashboards. If the AI is the consumer, text is sufficient.
2. **Use the existing component library.** FlowChart is the shared flow visualization component, reusable across all games. Everything else (BarChart, StackedBar, ProgressRing, Heatmap, DataTable, FactorChain, Sparkline) already exists.
3. **Custom layout, Svelte renders.** FlowChart has its own layout engine (BFS depth ordering, overlap resolution). All elements are Svelte `{#each}` loops. No external layout library dependencies.
4. **Icons are progressive enhancement.** Every view works in text-label mode. Icons add visual clarity but aren't required for comprehension.
5. **Text fallback is mandatory.** All reference modules return structured data + narrative text. Views enhance the conversation but the text response must stand alone for hosts without MCP Apps support.
6. **Tooltips follow the existing pattern.** DOM overlay positioned relative to SVG elements, consistent with BarChart/RadarChart tooltip behavior.
7. **`updateModelContext()` for interactions.** If a user clicks a node in the production flow, the view calls `updateModelContext()` to tell the AI what the user is looking at. No `callServerTool` from views.

## Roadmap

### Phase 1: Core Integration

- [ ] Lua mod: `control.lua` with `on_nth_tick` state dump
- [ ] Mod sections: `game_overview`, `production_flow`, `machines`, `research`, `resources`, `power`
- [ ] plugin.toml and daemon configuration for `script-output/savecraft/` watching
- [ ] WASM pass-through parser (JSON -> ndjson identity mapping)
- [ ] Publish mod on Factorio Mod Portal
- [ ] Reference datagen: `factorio --dump-data` parser + Go struct generation
- [ ] Reference modules: `recipe_lookup`, `ratio_calculator`
- [ ] Icon sprite sheet: extract via `--dump-icon-sprites`, upload to R2, build manifest
- [ ] `FactorioIcon` component (sprite + label modes)
- [ ] `ratio_calculator` FlowChart view (ProductionChain + MachineNode)

### Phase 2: Full Game Companion

- [ ] Mod sections: `fluids`, `logistics`, `trains`, `defenses`, `inventory`, `alerts`
- [ ] Reference modules: `oil_balancer`, `power_calculator`, `evolution_tracker`
- [ ] Reference modules: `blueprint_analyzer`, `module_optimizer`
- [ ] Reference module: `tech_tree_navigator`
- [ ] `oil_balancer` FlowSankey view (d3-sankey + Svelte SVG)
- [ ] `blueprint_analyzer` report view (DataTable + BarChart + FactorChain)
- [ ] `evolution_tracker` threat timeline view (ProgressRing + StackedBar + Timeline)
- [ ] `production_flow` factory health dashboard view (BarChart + Sparkline + badges)
- [ ] `power_calculator` layout view (StackedBar + nuclear schematic)

### Phase 3: Space Age

- [ ] Mod sections: `surfaces` (planets + space platforms)
- [ ] Reference module: `quality_calculator`
- [ ] Space platform data: routes, cargo, thrusters, asteroid collectors
- [ ] Per-surface production_flow and machines breakdowns
- [ ] `quality_calculator` distribution view (Heatmap + BarChart)

### Phase 4: Advanced Features

- [ ] Mod section: `blueprint_library` (index export)
- [ ] Reference module: `train_throughput`
- [ ] Multiplayer / dedicated server support via RCON
- [ ] Modded recipe support (mod exports active recipe data for non-vanilla games)

### Deferred / Under Consideration

These were evaluated during design and deprioritized. They may be added based on user demand.

| Feature | Why Deferred | Add When |
|---------|-------------|----------|
| Spatial layout analysis ("fix my spaghetti") | Requires exporting entity positions, creating massive data. Aggregate stats + AI strategic advice covers 80% of the value. | When Savecraft has a view system that can render factory maps |
| Logistics bot optimizer (reference module) | Charge pad throughput math is simpler than other modules. AI handles it from `logistics` section data. | If bot throughput questions are frequent |
| Pollution spread modeler (reference module) | Depends on chunk absorption rates and terrain. Complex simulation for marginal value over `evolution_tracker`. | If players want pollution prediction specifically |
| Combat DPS calculator (reference module) | Factorio combat has fewer variables than RimWorld. Turret stats could be added to `recipe_lookup`. | If defense analysis needs precise DPS calculations |
| Circuit network analyzer | Would need to export wire connections and combinator logic. Extremely complex data, niche audience. | When circuit networks become a common pain point |
| Achievement-safe console path | `/silent-command` via RCON doesn't disable achievements in multiplayer. Alternative to mod for server operators. | Multiplayer support phase |

## Key Numbers Reference

Embedded in reference module data but useful for development:

| Constant | Value |
|----------|-------|
| Yellow belt throughput | 15 items/s (7.5/lane) |
| Red belt throughput | 30 items/s (15/lane) |
| Blue belt throughput | 45 items/s (22.5/lane) |
| Green belt throughput (Space Age) | 60 items/s (30/lane) |
| AM1 crafting speed | 0.5 |
| AM2 crafting speed | 0.75 (2 module slots) |
| AM3 crafting speed | 1.25 (4 module slots) |
| Electric mining drill speed | 0.5/s |
| Nuclear reactor base output | 40 MW thermal |
| Nuclear neighbor bonus | +100% per adjacent fueled reactor |
| Solar panel average output | ~42 kW (60 kW peak) |
| Solar:accumulator ratio | 25:21 |
| Boiler:engine ratio | 1:2 |
| Offshore pump:boiler ratio | 1:20 |
| Beacon transmission (normal quality) | 1.5 / sqrt(n) per beacon |
| Evolution squashing | factor = raw / (1 + raw) |
| Behemoth spawn threshold | evolution >= 0.9 |
| Quality upgrade re-roll chance | 10% per tier |
| Recycler material return | 25% |
