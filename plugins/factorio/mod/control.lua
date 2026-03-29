-- Savecraft State Export for Factorio 2.0+
-- Writes game state as JSON to script-output/savecraft/state.json
-- The Savecraft daemon watches this file and pushes data to the server.

local STATS_INTERVAL = 300   -- ticks (5s) — lightweight stats: production, research, power
local ENTITY_INTERVAL = 1800 -- ticks (30s) — heavier entity queries: machines, resources

local EXPORT_PATH = "savecraft/state.json"

-- Cached entity data (refreshed every ENTITY_INTERVAL ticks)
local cached_machines = nil
local cached_resources = nil

-- ─── Identity ────────────────────────────────────────────────────────────────

--- Generate a stable save identity, persisted in storage across sessions.
local function ensure_save_id()
  if not storage.savecraft_save_id then
    storage.savecraft_save_id = string.format(
      "%x-%x-%x",
      game.tick,
      math.random(0x10000, 0xFFFFF),
      math.random(0x10000, 0xFFFFF)
    )
  end
  return storage.savecraft_save_id
end

-- ─── game_overview ───────────────────────────────────────────────────────────

local function collect_mods()
  local mods = {}
  for name, version in pairs(script.active_mods) do
    mods[#mods + 1] = { name = name, version = version }
  end
  return mods
end

local function collect_surface_names()
  local names = {}
  for _, surface in pairs(game.surfaces) do
    names[#names + 1] = surface.name
  end
  return names
end

local function active_surface()
  local players = game.connected_players
  if #players > 0 then
    return players[1].surface.name
  end
  return "nauvis"
end

local function collect_game_overview()
  local force = game.forces["player"]
  local ticks = game.ticks_played
  local hours = ticks / (60 * 60 * 60)

  return {
    save_id = ensure_save_id(),
    game_version = script.active_mods["base"] or "unknown",
    ticks_played = ticks,
    hours_played = math.floor(hours * 100) / 100,
    difficulty_settings = {
      recipe_difficulty = game.difficulty_settings.recipe_difficulty,
      technology_difficulty = game.difficulty_settings.technology_difficulty,
    },
    mods = collect_mods(),
    rocket_launches = force and force.rockets_launched or 0,
    surfaces = collect_surface_names(),
    active_surface = active_surface(),
  }
end

-- ─── production_flow ─────────────────────────────────────────────────────────

--- Collect production/consumption rates for items and fluids.
-- Uses LuaFlowStatistics with precision_index=2 (1-minute window) for rates.
local function collect_production_flow()
  local force = game.forces["player"]
  if not force then return { items = {}, fluids = {}, top_deficits = {}, top_surpluses = {} } end

  local item_stats = force.item_production_statistics
  local fluid_stats = force.fluid_production_statistics

  local items = {}
  local deficits = {}
  local surpluses = {}

  -- Iterate all item prototypes, collect non-zero entries.
  for name, _ in pairs(prototypes.item) do
    local produced = item_stats.get_output_count(name)
    local consumed = item_stats.get_input_count(name)
    if produced > 0 or consumed > 0 then
      -- Flow count with precision_index 2 = per-minute rate.
      local produced_per_min = item_stats.get_flow_count{
        name = name, input = false, precision_index = 2, count = true,
      }
      local consumed_per_min = item_stats.get_flow_count{
        name = name, input = true, precision_index = 2, count = true,
      }
      local net = produced_per_min - consumed_per_min
      items[name] = {
        produced_total = produced,
        consumed_total = consumed,
        produced_per_min = math.floor(produced_per_min * 10) / 10,
        consumed_per_min = math.floor(consumed_per_min * 10) / 10,
      }
      if net < -0.1 then
        deficits[#deficits + 1] = { name = name, net = net }
      elseif net > 0.1 then
        surpluses[#surpluses + 1] = { name = name, net = net }
      end
    end
  end

  -- Fluids
  local fluids = {}
  for name, _ in pairs(prototypes.fluid) do
    local produced = fluid_stats.get_output_count(name)
    local consumed = fluid_stats.get_input_count(name)
    if produced > 0 or consumed > 0 then
      local produced_per_min = fluid_stats.get_flow_count{
        name = name, input = false, precision_index = 2, count = true,
      }
      local consumed_per_min = fluid_stats.get_flow_count{
        name = name, input = true, precision_index = 2, count = true,
      }
      fluids[name] = {
        produced_total = produced,
        consumed_total = consumed,
        produced_per_min = math.floor(produced_per_min * 10) / 10,
        consumed_per_min = math.floor(consumed_per_min * 10) / 10,
      }
    end
  end

  -- Sort deficits and surpluses by magnitude, take top 5.
  table.sort(deficits, function(a, b) return a.net < b.net end)
  table.sort(surpluses, function(a, b) return a.net > b.net end)

  local top_deficits = {}
  for i = 1, math.min(5, #deficits) do
    top_deficits[i] = deficits[i].name
  end
  local top_surpluses = {}
  for i = 1, math.min(5, #surpluses) do
    top_surpluses[i] = surpluses[i].name
  end

  return {
    items = items,
    fluids = fluids,
    top_deficits = top_deficits,
    top_surpluses = top_surpluses,
  }
end

-- ─── machines ────────────────────────────────────────────────────────────────

--- Collect active machines grouped by recipe with module tallies.
-- This is expensive (entity scan), cached and refreshed every ENTITY_INTERVAL.
local function collect_machines()
  local by_recipe = {}
  local by_type = {}
  local beacon_count = 0

  for _, surface in pairs(game.surfaces) do
    -- Crafting machines
    local crafters = surface.find_entities_filtered{
      type = {
        "assembling-machine", "furnace", "chemical-plant",
        "oil-refinery", "rocket-silo",
      },
    }
    for _, entity in pairs(crafters) do
      local type_name = entity.name
      by_type[type_name] = (by_type[type_name] or 0) + 1

      local recipe = entity.get_recipe()
      if recipe then
        local recipe_name = recipe.name
        local entry = by_recipe[recipe_name]
        if not entry then
          entry = { machine_type = type_name, count = 0, modules = {} }
          by_recipe[recipe_name] = entry
        end
        entry.count = entry.count + 1

        -- Tally modules
        local inv = entity.get_module_inventory()
        if inv then
          for i = 1, #inv do
            local stack = inv[i]
            if stack.valid_for_read then
              local mod_name = stack.name
              entry.modules[mod_name] = (entry.modules[mod_name] or 0) + 1
            end
          end
        end
      end
    end

    -- Beacons
    local beacons = surface.find_entities_filtered{ type = "beacon" }
    beacon_count = beacon_count + #beacons
  end

  return {
    by_recipe = by_recipe,
    by_type = by_type,
    beacon_count = beacon_count,
  }
end

-- ─── research ────────────────────────────────────────────────────────────────

local function collect_research()
  local force = game.forces["player"]
  if not force then return { completed = {}, completed_count = 0, infinite_levels = {} } end

  -- Current research
  local current = nil
  if force.current_research then
    local tech = force.current_research
    local ingredients = {}
    for _, ing in pairs(tech.research_unit_ingredients) do
      ingredients[#ingredients + 1] = { ing.name, ing.amount }
    end
    current = {
      name = tech.name,
      progress = math.floor(force.research_progress * 1000) / 1000,
      cost_per_unit = tech.research_unit_count,
      unit_count = tech.research_unit_count,
      ingredients = ingredients,
    }
  end

  -- Research queue
  local queue = {}
  if force.research_queue_enabled then
    for _, tech in pairs(force.research_queue) do
      queue[#queue + 1] = tech.name
    end
  end

  -- Completed technologies and infinite research levels
  local completed = {}
  local completed_count = 0
  local infinite_levels = {}
  local total_available = 0

  for name, tech in pairs(force.technologies) do
    if tech.enabled then
      total_available = total_available + 1
    end
    if tech.researched then
      completed[#completed + 1] = name
      completed_count = completed_count + 1
      -- Infinite research has level > 1 when researched multiple times
      if tech.prototype.max_level == math.huge or (tech.level and tech.level > 1) then
        infinite_levels[name] = tech.level
      end
    end
  end

  return {
    current = current,
    queue = queue,
    completed = completed,
    completed_count = completed_count,
    total_available = total_available,
    infinite_levels = infinite_levels,
  }
end

-- ─── resources ───────────────────────────────────────────────────────────────

--- Collect resource patches that have mining drills on them.
-- Expensive (entity scan), cached and refreshed every ENTITY_INTERVAL.
local function collect_resources()
  local force = game.forces["player"]
  local patches = {}
  -- Key: "surface:type:chunk_x:chunk_y" to cluster drills into patches
  local patch_map = {}

  for _, surface in pairs(game.surfaces) do
    local drills = surface.find_entities_filtered{
      type = "mining-drill",
      force = "player",
    }

    for _, drill in pairs(drills) do
      local target = drill.mining_target
      if target and target.valid then
        local resource_type = target.name
        -- Cluster by chunk (32-tile grid) as a rough patch grouping
        local chunk_x = math.floor(target.position.x / 32)
        local chunk_y = math.floor(target.position.y / 32)
        local key = surface.name .. ":" .. resource_type .. ":" .. chunk_x .. ":" .. chunk_y

        local patch = patch_map[key]
        if not patch then
          patch = {
            type = resource_type,
            surface = surface.name,
            center_x = 0,
            center_y = 0,
            remaining = 0,
            drills = 0,
            drill_positions = 0,
          }
          patch_map[key] = patch
        end

        patch.drills = patch.drills + 1
        patch.center_x = patch.center_x + drill.position.x
        patch.center_y = patch.center_y + drill.position.y
        patch.remaining = patch.remaining + target.amount
      end
    end
  end

  -- Compute centers and format output
  for _, patch in pairs(patch_map) do
    if patch.drills > 0 then
      patches[#patches + 1] = {
        type = patch.type,
        surface = patch.surface,
        center = {
          x = math.floor(patch.center_x / patch.drills),
          y = math.floor(patch.center_y / patch.drills),
        },
        remaining = patch.remaining,
        drills = patch.drills,
      }
    end
  end

  return {
    patches = patches,
    mining_productivity_bonus = force and force.mining_drill_productivity_bonus or 0,
  }
end

-- ─── power ───────────────────────────────────────────────────────────────────

local function collect_power()
  local surfaces = {}

  for _, surface in pairs(game.surfaces) do
    local generators = {}
    local total_generation = 0
    local total_consumption = 0

    -- Steam engines / generators
    local steam = surface.find_entities_filtered{ type = "generator", force = "player" }
    if #steam > 0 then
      local mw = 0
      for _, e in pairs(steam) do
        mw = mw + (e.energy_generated_last_tick or 0) * 60 / 1000000
      end
      generators["steam-engine"] = { count = #steam, mw = math.floor(mw * 10) / 10 }
      total_generation = total_generation + mw
    end

    -- Solar panels
    local solar = surface.find_entities_filtered{ type = "solar-panel", force = "player" }
    if #solar > 0 then
      local mw = 0
      for _, e in pairs(solar) do
        mw = mw + (e.energy_generated_last_tick or 0) * 60 / 1000000
      end
      generators["solar-panel"] = { count = #solar, mw = math.floor(mw * 10) / 10 }
      total_generation = total_generation + mw
    end

    -- Reactors (thermal, not directly electric — but good to count)
    local reactors = surface.find_entities_filtered{ type = "reactor", force = "player" }
    if #reactors > 0 then
      local mw = 0
      for _, e in pairs(reactors) do
        -- Reactor neighbour bonus is reflected in the actual energy output
        mw = mw + (e.energy_generated_last_tick or 0) * 60 / 1000000
      end
      generators["nuclear-reactor"] = { count = #reactors, mw = math.floor(mw * 10) / 10 }
      total_generation = total_generation + mw
    end

    -- Accumulators
    local accumulators = surface.find_entities_filtered{ type = "accumulator", force = "player" }
    local acc_data = nil
    if #accumulators > 0 then
      local charge = 0
      local capacity = 0
      for _, e in pairs(accumulators) do
        charge = charge + e.energy
        capacity = capacity + (e.electric_buffer_size or 0)
      end
      acc_data = {
        count = #accumulators,
        charge_mj = math.floor(charge / 1000000 * 10) / 10,
        capacity_mj = math.floor(capacity / 1000000 * 10) / 10,
      }
    end

    -- Only include surfaces that have power infrastructure
    if total_generation > 0 or #accumulators > 0 then
      -- Estimate consumption from all electric entities on this surface
      -- (precise consumption requires summing all entity drain, which is very expensive;
      --  we approximate via the electric network if available)
      local consumption_mw = 0
      if #steam > 0 then
        -- Use the first generator's electric network statistics if available
        local net = steam[1].electric_network_statistics
        if net then
          consumption_mw = net.get_flow_count{
            name = "consumption", input = true, precision_index = 2, count = true,
          } / 1000000 * 60
        end
      end

      surfaces[surface.name] = {
        generation_mw = math.floor(total_generation * 10) / 10,
        consumption_mw = math.floor(consumption_mw * 10) / 10,
        satisfaction = consumption_mw > 0 and math.floor(total_generation / consumption_mw * 100) / 100 or 1.0,
        generators = generators,
        accumulators = acc_data,
      }
    end
  end

  return { surfaces = surfaces }
end

-- ─── Export ──────────────────────────────────────────────────────────────────

--- Build the full export payload.
local function build_export(include_entities)
  local overview = collect_game_overview()

  -- Update entity caches if requested
  if include_entities then
    cached_machines = collect_machines()
    cached_resources = collect_resources()
  end

  local summary = string.format(
    "Factorio — %.1f hours, %d rockets launched",
    overview.hours_played,
    overview.rocket_launches
  )

  local sections = {
    game_overview = {
      description = "Map identity and high-level game state",
      data = overview,
    },
    production_flow = {
      description = "Per-item and per-fluid production and consumption rates",
      data = collect_production_flow(),
    },
    research = {
      description = "Current research, queue, completed technologies, and infinite research levels",
      data = collect_research(),
    },
    power = {
      description = "Per-surface power generation, consumption, and satisfaction",
      data = collect_power(),
    },
  }

  -- Include entity-scanned sections only when cached data exists
  if cached_machines then
    sections.machines = {
      description = "Active machines grouped by recipe with module tallies",
      data = cached_machines,
    }
  end
  if cached_resources then
    sections.resources = {
      description = "Resource patches with mining drills and remaining amounts",
      data = cached_resources,
    }
  end

  return {
    identity = {
      save_name = overview.save_id,
      game_id = "factorio",
    },
    summary = summary,
    sections = sections,
  }
end

--- Write the export to disk.
local function export_state(include_entities)
  local payload = build_export(include_entities)
  local json = helpers.table_to_json(payload)
  helpers.write_file(EXPORT_PATH, json .. "\n", false)
end

-- Lightweight stats every 5 seconds
script.on_nth_tick(STATS_INTERVAL, function()
  export_state(false)
end)

-- Full entity scan every 30 seconds
script.on_nth_tick(ENTITY_INTERVAL, function()
  export_state(true)
end)

-- Export immediately on key game events (with entity data)
script.on_event(defines.events.on_research_finished, function()
  export_state(true)
end)
script.on_event(defines.events.on_rocket_launched, function()
  export_state(true)
end)

-- Generate save ID on new map or first load
script.on_init(function()
  ensure_save_id()
  export_state(true)
end)

script.on_load(function()
  -- storage is restored from save automatically; no action needed
end)
