-- Savecraft State Export for Factorio 2.0+
-- Writes game state as JSON to script-output/savecraft/state.json
-- The Savecraft daemon watches this file and pushes data to the server.

local STATS_INTERVAL = 300   -- ticks (5s) — lightweight stats: production, research, power
local ENTITY_INTERVAL = 1800 -- ticks (30s) — heavier entity queries: machines, resources

local EXPORT_PATH = "savecraft/state.json"

-- Cached entity data (refreshed every ENTITY_INTERVAL ticks)
local cached_machines = nil
local cached_resources = nil
local cached_power = nil
local cached_fluids = nil
local cached_logistics = nil
local cached_trains = nil
local cached_defenses = nil

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
      technology_price_multiplier = game.difficulty_settings.technology_price_multiplier,
      spoil_time_modifier = game.difficulty_settings.spoil_time_modifier,
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

  -- In Factorio 2.0, production stats are per-surface. Aggregate across all surfaces.
  local items = {}
  local fluids = {}

  for _, surface in pairs(game.surfaces) do
    local item_stats = force.get_item_production_statistics(surface)
    local fluid_stats = force.get_fluid_production_statistics(surface)

    for name, _ in pairs(prototypes.item) do
      local produced = item_stats.get_output_count(name)
      local consumed = item_stats.get_input_count(name)
      if produced > 0 or consumed > 0 then
        local produced_per_min = item_stats.get_flow_count{
          name = name, category = "output", precision_index = defines.flow_precision_index.one_minute, count = true,
        }
        local consumed_per_min = item_stats.get_flow_count{
          name = name, category = "input", precision_index = defines.flow_precision_index.one_minute, count = true,
        }
        local entry = items[name]
        if entry then
          entry.produced_total = entry.produced_total + produced
          entry.consumed_total = entry.consumed_total + consumed
          entry.produced_per_min = entry.produced_per_min + produced_per_min
          entry.consumed_per_min = entry.consumed_per_min + consumed_per_min
        else
          items[name] = {
            produced_total = produced,
            consumed_total = consumed,
            produced_per_min = produced_per_min,
            consumed_per_min = consumed_per_min,
          }
        end
      end
    end

    for name, _ in pairs(prototypes.fluid) do
      local produced = fluid_stats.get_output_count(name)
      local consumed = fluid_stats.get_input_count(name)
      if produced > 0 or consumed > 0 then
        local produced_per_min = fluid_stats.get_flow_count{
          name = name, category = "output", precision_index = defines.flow_precision_index.one_minute, count = true,
        }
        local consumed_per_min = fluid_stats.get_flow_count{
          name = name, category = "input", precision_index = defines.flow_precision_index.one_minute, count = true,
        }
        local entry = fluids[name]
        if entry then
          entry.produced_total = entry.produced_total + produced
          entry.consumed_total = entry.consumed_total + consumed
          entry.produced_per_min = entry.produced_per_min + produced_per_min
          entry.consumed_per_min = entry.consumed_per_min + consumed_per_min
        else
          fluids[name] = {
            produced_total = produced,
            consumed_total = consumed,
            produced_per_min = produced_per_min,
            consumed_per_min = consumed_per_min,
          }
        end
      end
    end
  end

  -- Round rates after aggregation
  local deficits = {}
  local surpluses = {}
  for name, entry in pairs(items) do
    entry.produced_per_min = math.floor(entry.produced_per_min * 10) / 10
    entry.consumed_per_min = math.floor(entry.consumed_per_min * 10) / 10
    local net = entry.produced_per_min - entry.consumed_per_min
    if net < -0.1 then
      deficits[#deficits + 1] = { name = name, net = net }
    elseif net > 0.1 then
      surpluses[#surpluses + 1] = { name = name, net = net }
    end
  end
  for _, entry in pairs(fluids) do
    entry.produced_per_min = math.floor(entry.produced_per_min * 10) / 10
    entry.consumed_per_min = math.floor(entry.consumed_per_min * 10) / 10
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

  -- Research queue (always available in Factorio 2.0)
  local queue = {}
  for _, tech_id in pairs(force.research_queue) do
    queue[#queue + 1] = tech_id
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

    -- Solar panels (no energy_generated_last_tick — report count only)
    local solar = surface.find_entities_filtered{ type = "solar-panel", force = "player" }
    if #solar > 0 then
      generators["solar-panel"] = { count = #solar }
    end

    -- Reactors (produce heat, not electricity — report count and neighbour bonus)
    local reactors = surface.find_entities_filtered{ type = "reactor", force = "player" }
    if #reactors > 0 then
      local total_bonus = 0
      for _, e in pairs(reactors) do
        total_bonus = total_bonus + (e.neighbour_bonus or 0)
      end
      generators["nuclear-reactor"] = {
        count = #reactors,
        avg_neighbour_bonus = math.floor(total_bonus / #reactors * 100) / 100,
      }
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
      -- Try to get consumption from an electric pole's network statistics
      local poles = surface.find_entities_filtered{ type = "electric-pole", force = "player", limit = 1 }
      if #poles > 0 then
        local ok, net = pcall(function() return poles[1].electric_network_statistics end)
        if ok and net then
          local ok2, val = pcall(function()
            return net.get_flow_count{
              name = "consumption", input = true, precision_index = 2, count = true,
            }
          end)
          if ok2 and val then
            consumption_mw = val / 1000000 * 60
          end
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

-- ─── fluids ─────────────────────────────────────────────────────────────────

--- Collect oil processing setup and fluid tank levels.
-- Derives refinery/chemical-plant counts from cached_machines to avoid duplicate scans.
-- Only the tank scan is a new entity query.
local function collect_fluids()
  -- Extract refinery and chemical plant recipe counts from the machines cache
  local refineries = {}
  local chemical_plants = {}
  if cached_machines and cached_machines.by_recipe then
    for recipe_name, entry in pairs(cached_machines.by_recipe) do
      if entry.machine_type == "oil-refinery" then
        refineries[recipe_name] = entry.count
      elseif entry.machine_type == "chemical-plant" then
        chemical_plants[recipe_name] = entry.count
      end
    end
  end

  -- Storage tanks — aggregate fluid levels by fluid type (unique scan)
  local tank_levels = {}
  for _, surface in pairs(game.surfaces) do
    local tanks = surface.find_entities_filtered{ type = "storage-tank", force = "player" }
    for _, tank in pairs(tanks) do
      local fluidbox = tank.fluidbox
      if fluidbox and #fluidbox > 0 then
        local fluid = fluidbox[1]
        if fluid then
          local name = fluid.name
          local entry = tank_levels[name]
          if not entry then
            entry = { current = 0, capacity = 0 }
            tank_levels[name] = entry
          end
          entry.current = entry.current + fluid.amount
          entry.capacity = entry.capacity + fluidbox.get_capacity(1)
        end
      end
    end
  end

  -- Round tank levels
  for _, entry in pairs(tank_levels) do
    entry.current = math.floor(entry.current)
    entry.capacity = math.floor(entry.capacity)
  end

  return {
    refineries = refineries,
    chemical_plants = chemical_plants,
    tank_levels = tank_levels,
  }
end

-- ─── logistics ──────────────────────────────────────────────────────────────

--- Collect per-surface roboport coverage, bot counts, and logistics network state.
-- Expensive (entity scan), cached and refreshed every ENTITY_INTERVAL.
local function collect_logistics()
  local surfaces = {}

  for _, surface in pairs(game.surfaces) do
    local force = game.forces["player"]
    if not force then break end

    local networks = force.logistic_networks[surface.name]
    if networks and #networks > 0 then
      local total_logistic = 0
      local avail_logistic = 0
      local total_construction = 0
      local avail_construction = 0
      local roboport_count = 0

      for _, network in pairs(networks) do
        total_logistic = total_logistic + network.all_logistic_robots
        avail_logistic = avail_logistic + network.available_logistic_robots
        total_construction = total_construction + network.all_construction_robots
        avail_construction = avail_construction + network.available_construction_robots
        roboport_count = roboport_count + #network.cells
      end

      -- Count logistics chest types (single scan, 2.0 names: X-chest)
      local chest_names = {
        ["passive-provider-chest"] = "passive-provider",
        ["requester-chest"] = "requester",
        ["storage-chest"] = "storage",
        ["buffer-chest"] = "buffer",
        ["active-provider-chest"] = "active-provider",
      }
      local logistic_chests = {}
      local chests = surface.find_entities_filtered{
        name = { "passive-provider-chest", "requester-chest", "storage-chest",
                 "buffer-chest", "active-provider-chest" },
        force = "player",
      }
      for _, chest in pairs(chests) do
        local label = chest_names[chest.name]
        if label then
          logistic_chests[label] = (logistic_chests[label] or 0) + 1
        end
      end

      surfaces[surface.name] = {
        roboports = roboport_count,
        logistic_bots = { total = total_logistic, available = avail_logistic },
        construction_bots = { total = total_construction, available = avail_construction },
        logistic_chests = logistic_chests,
      }
    end
  end

  return { surfaces = surfaces }
end

-- ─── trains ─────────────────────────────────────────────────────────────────

local TRAIN_STATE_NAMES = {}
for _, pair in ipairs({
  { defines.train_state.on_the_path, "on_the_path" },
  { defines.train_state.no_schedule, "no_schedule" },
  { defines.train_state.no_path, "no_path" },
  { defines.train_state.arrive_signal, "arrive_signal" },
  { defines.train_state.wait_signal, "wait_signal" },
  { defines.train_state.arrive_station, "arrive_station" },
  { defines.train_state.wait_station, "wait_station" },
  { defines.train_state.manual_control_stop, "manual_control_stop" },
  { defines.train_state.manual_control, "manual_control" },
  { defines.train_state.destination_full, "destination_full" },
}) do
  if pair[1] ~= nil then TRAIN_STATE_NAMES[pair[1]] = pair[2] end
end

--- Collect train list with composition, schedule, cargo, and fuel.
-- Also collects train stops with their train limits.
-- Expensive (entity scan), cached and refreshed every ENTITY_INTERVAL.
local function collect_trains()
  local train_list = {}

  -- Factorio 2.0: game.train_manager.get_trains({}) returns all trains
  local all_trains = game.train_manager and game.train_manager.get_trains({}) or {}
  for _, train in pairs(all_trains) do
    if train.valid then
      -- 2.0: train.locomotives returns {front_movers=[], back_movers=[]}, not a flat array
      local front = train.locomotives.front_movers or {}
      local back = train.locomotives.back_movers or {}
      local locos = #front + #back
      local wagons = #train.cargo_wagons + #train.fluid_wagons
      local composition = locos .. "-" .. wagons

      local state = TRAIN_STATE_NAMES[train.state] or "unknown"

      -- Schedule stops
      local schedule = {}
      if train.schedule and train.schedule.records then
        for _, record in pairs(train.schedule.records) do
          local wait_desc = "unknown"
          if record.wait_conditions and #record.wait_conditions > 0 then
            -- Use the first wait condition type as the summary
            wait_desc = record.wait_conditions[1].type
          end
          schedule[#schedule + 1] = {
            station = record.station or "unknown",
            wait = wait_desc,
          }
        end
      end

      -- Cargo contents (items across all cargo wagons)
      -- 2.0: get_contents() returns ItemWithQualityCount[] = {name, count, quality}
      local cargo = {}
      for _, wagon in pairs(train.cargo_wagons) do
        local inv = wagon.get_inventory(defines.inventory.cargo_wagon)
        if inv then
          local contents = inv.get_contents()
          for _, item in pairs(contents) do
            cargo[item.name] = (cargo[item.name] or 0) + item.count
          end
        end
      end

      -- Fuel type from first locomotive
      -- 2.0: locomotives.front_movers + back_movers
      local fuel = nil
      local first_loco = front[1] or back[1]
      if first_loco then
        local fuel_inv = first_loco.get_inventory(defines.inventory.fuel)
        if fuel_inv and not fuel_inv.is_empty() then
          for i = 1, #fuel_inv do
            local stack = fuel_inv[i]
            if stack.valid_for_read then
              fuel = stack.name
              break
            end
          end
        end
      end

      train_list[#train_list + 1] = {
        id = train.id,
        composition = composition,
        state = state,
        schedule = schedule,
        cargo = cargo,
        fuel = fuel,
      }
    end
  end

  -- Train stops
  local station_list = {}
  for _, surface in pairs(game.surfaces) do
    local stops = surface.find_entities_filtered{ type = "train-stop", force = "player" }
    for _, stop in pairs(stops) do
      station_list[#station_list + 1] = {
        name = stop.backer_name,
        position = { x = math.floor(stop.position.x), y = math.floor(stop.position.y) },
        train_limit = stop.trains_limit,
      }
    end
  end

  return {
    trains = train_list,
    stations = station_list,
  }
end

-- ─── defenses ───────────────────────────────────────────────────────────────

--- Collect evolution factor, turret counts, wall count, and nearby enemy bases.
-- Expensive (entity scan), cached and refreshed every ENTITY_INTERVAL.
local function collect_defenses()
  local force = game.forces["player"]
  if not force then return { evolution = {}, turrets = {}, walls = 0 } end

  -- Evolution factor with source breakdown (2.0: all per-surface methods)
  local surface = game.surfaces[active_surface()]
  local evo_factor = force.get_evolution_factor(surface)

  local evolution = {
    factor = math.floor(evo_factor * 10000) / 10000,
    time_factor = math.floor(force.get_evolution_factor_by_time(surface) * 10000) / 10000,
    pollution_factor = math.floor(force.get_evolution_factor_by_pollution(surface) * 10000) / 10000,
    kill_factor = math.floor(force.get_evolution_factor_by_killing_spawners(surface) * 10000) / 10000,
  }

  -- Turret counts by type (single scan for all turret types)
  local turret_types = {}
  local wall_count = 0
  for _, surf in pairs(game.surfaces) do
    local turrets = surf.find_entities_filtered{
      type = { "ammo-turret", "electric-turret", "fluid-turret", "artillery-turret" },
      force = "player",
    }
    for _, t in pairs(turrets) do
      turret_types[t.name] = (turret_types[t.name] or 0) + 1
    end

    -- Walls — count only, don't materialize entities
    wall_count = wall_count + surf.count_entities_filtered{ type = "wall", force = "player" }
  end

  -- Nearby enemy bases (spawners within 256 tiles of player)
  local enemy_bases = {}
  local players = game.connected_players
  if #players > 0 then
    local player = players[1]
    local spawners = player.surface.find_entities_filtered{
      type = "unit-spawner",
      force = "enemy",
      position = player.position,
      radius = 256,
    }
    for _, spawner in pairs(spawners) do
      local dx = spawner.position.x - player.position.x
      local dy = spawner.position.y - player.position.y
      local dist = math.floor(math.sqrt(dx * dx + dy * dy))

      -- Cardinal direction
      local angle = math.atan2(dy, dx)
      local dirs = { "east", "southeast", "south", "southwest", "west", "northwest", "north", "northeast" }
      local idx = math.floor((angle + math.pi) / (2 * math.pi) * 8 + 0.5) % 8 + 1
      local direction = dirs[idx]

      enemy_bases[#enemy_bases + 1] = {
        distance = dist,
        direction = direction,
        type = spawner.name, -- e.g. "biter-spawner", "spitter-spawner"
      }
    end
    -- Sort by distance, keep closest 20
    table.sort(enemy_bases, function(a, b) return a.distance < b.distance end)
    if #enemy_bases > 20 then
      local trimmed = {}
      for i = 1, 20 do trimmed[i] = enemy_bases[i] end
      enemy_bases = trimmed
    end
  end

  -- Total pollution on the active surface (O(1) via statistics, not chunk iteration)
  local total_pollution = 0
  if surface and surface.pollution_statistics then
    total_pollution = surface.pollution_statistics.output_counts["pollution"] or 0
  end

  return {
    evolution = evolution,
    turrets = turret_types,
    walls = wall_count,
    enemy_bases_nearby = enemy_bases,
    total_pollution = math.floor(total_pollution),
  }
end

-- ─── inventory ──────────────────────────────────────────────────────────────

--- Collect player inventory, equipment, crafting queue, and position.
-- Lightweight — computed fresh every STATS_INTERVAL.
local function collect_inventory()
  local players = game.connected_players
  if #players == 0 then return {} end

  local player = players[1]
  local main_inv = player.get_inventory(defines.inventory.character_main)
  local main = {}
  if main_inv then
    local contents = main_inv.get_contents()
    for _, item in pairs(contents) do
      main[item.name] = item.count
    end
  end

  -- Armor name
  local armor = nil
  local armor_inv = player.get_inventory(defines.inventory.character_armor)
  if armor_inv and not armor_inv.is_empty() then
    local stack = armor_inv[1]
    if stack.valid_for_read then
      armor = stack.name
    end
  end

  -- Equipment grid contents
  local equipment_grid = {}
  if player.character and player.character.grid then
    local grid = player.character.grid
    for _, eq in pairs(grid.equipment) do
      equipment_grid[#equipment_grid + 1] = eq.name
    end
  end

  -- Crafting queue
  local crafting_queue = {}
  if player.crafting_queue then
    for _, item in pairs(player.crafting_queue) do
      crafting_queue[#crafting_queue + 1] = {
        recipe = item.recipe,
        count = item.count,
      }
    end
  end

  return {
    player = {
      main = main,
      armor = armor,
      equipment_grid = equipment_grid,
      crafting_queue = crafting_queue,
      position = {
        x = math.floor(player.position.x),
        y = math.floor(player.position.y),
        surface = player.surface.name,
      },
    },
  }
end

-- ─── alerts ─────────────────────────────────────────────────────────────────

--- Collect active game alerts — high-signal ephemeral data.
-- Lightweight — computed fresh every STATS_INTERVAL.
local function collect_alerts()
  local players = game.connected_players
  if #players == 0 then return {} end

  local player = players[1]
  local alert_counts = {}

  -- Map Factorio 2.0 alert types to readable names
  local alert_names = {
    [defines.alert_type.entity_destroyed] = "entity_destroyed",
    [defines.alert_type.entity_under_attack] = "entity_under_attack",
    [defines.alert_type.no_material_for_construction] = "no_material_for_construction",
    [defines.alert_type.no_storage] = "no_storage",
    [defines.alert_type.no_roboport_storage] = "no_roboport_storage",
    [defines.alert_type.not_enough_construction_robots] = "not_enough_construction_robots",
    [defines.alert_type.not_enough_repair_packs] = "not_enough_repair_packs",
    [defines.alert_type.train_out_of_fuel] = "train_out_of_fuel",
    [defines.alert_type.train_no_path] = "train_no_path",
    [defines.alert_type.turret_fire] = "turret_fire",
    [defines.alert_type.turret_out_of_ammo] = "turret_out_of_ammo",
    -- Space Age alerts
    [defines.alert_type.no_platform_storage] = "no_platform_storage",
    [defines.alert_type.pipeline_overextended] = "pipeline_overextended",
    [defines.alert_type.collector_path_blocked] = "collector_path_blocked",
    [defines.alert_type.platform_tile_building_blocked] = "platform_tile_building_blocked",
    [defines.alert_type.unclaimed_cargo] = "unclaimed_cargo",
  }

  -- Single call fetches all alert types; iterate result to bucket by type
  -- Returns: surface_index → alert_type → Alert[]
  local all_alerts = player.get_alerts{}
  if all_alerts then
    for _, surface_alerts in pairs(all_alerts) do
      for alert_type, alert_list in pairs(surface_alerts) do
        local name = alert_names[alert_type]
        if name then
          alert_counts[name] = (alert_counts[name] or 0) + #alert_list
        end
      end
    end
  end

  return alert_counts
end

-- ─── Export ──────────────────────────────────────────────────────────────────

--- Build the full export payload.
local function build_export(include_entities)
  local overview = collect_game_overview()

  -- Update entity caches if requested (pcall each so one failure doesn't crash the export)
  if include_entities then
    local function safe_collect(name, fn)
      local ok, result = pcall(fn)
      if not ok then
        log("[savecraft] " .. name .. " failed: " .. tostring(result))
        return nil
      end
      return result
    end
    cached_machines = safe_collect("machines", collect_machines)
    cached_resources = safe_collect("resources", collect_resources)
    cached_power = safe_collect("power", collect_power)
    cached_fluids = safe_collect("fluids", collect_fluids)
    cached_logistics = safe_collect("logistics", collect_logistics)
    cached_trains = safe_collect("trains", collect_trains)
    cached_defenses = safe_collect("defenses", collect_defenses)
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
  }
  local collect_errors = {}
  local ok_flow, flow = pcall(collect_production_flow)
  if ok_flow and flow then
    sections.production_flow = {
      description = "Per-item and per-fluid production and consumption rates",
      data = flow,
    }
  elseif not ok_flow then
    collect_errors[#collect_errors + 1] = "production_flow: " .. tostring(flow)
  end
  local ok_research, research = pcall(collect_research)
  if ok_research and research then
    sections.research = {
      description = "Current research, queue, completed technologies, and infinite research levels",
      data = research,
    }
  elseif not ok_research then
    collect_errors[#collect_errors + 1] = "research: " .. tostring(research)
  end

  -- Include entity-scanned sections only when cached data exists
  if cached_power then
    sections.power = {
      description = "Per-surface power generation, consumption, and satisfaction",
      data = cached_power,
    }
  end
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
  if cached_fluids then
    sections.fluids = {
      description = "Oil processing setup, fluid tank levels, and fluid-specific production data",
      data = cached_fluids,
    }
  end
  if cached_logistics then
    sections.logistics = {
      description = "Per-surface roboport coverage, bot counts, and logistics network state",
      data = cached_logistics,
    }
  end
  if cached_trains then
    sections.trains = {
      description = "Train list with composition, schedule, cargo, and fuel; station list",
      data = cached_trains,
    }
  end
  if cached_defenses then
    sections.defenses = {
      description = "Evolution factor, turret counts, wall count, and nearby enemy bases",
      data = cached_defenses,
    }
  end

  -- Lightweight sections — computed fresh each tick (pcall for safety)
  local ok_inv, inv = pcall(collect_inventory)
  if ok_inv and inv then
    sections.inventory = {
      description = "Player inventory, equipment, crafting queue, and position",
      data = inv,
    }
  end
  local ok_alerts, alerts = pcall(collect_alerts)
  if ok_alerts and alerts then
    sections.alerts = {
      description = "Active game alerts — no fuel, no power, no storage, under attack",
      data = alerts,
    }
  end

  if #collect_errors > 0 then
    sections._errors = {
      description = "Sections that failed to collect (debug)",
      data = collect_errors,
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
