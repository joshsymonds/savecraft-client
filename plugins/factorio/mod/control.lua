-- Savecraft State Export for Factorio 2.0+
-- Writes game state as JSON to script-output/savecraft/state.json
-- The Savecraft daemon watches this file and pushes data to the server.

local EXPORT_INTERVAL = 300 -- ticks (5 seconds at 60 UPS)
local EXPORT_PATH = "savecraft/state.json"

--- Generate a stable save identity.
-- Called once on map creation, persisted in storage across sessions.
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

--- Collect the list of active mods with versions.
local function collect_mods()
  local mods = {}
  for name, version in pairs(script.active_mods) do
    mods[#mods + 1] = { name = name, version = version }
  end
  return mods
end

--- Collect the list of surfaces (planets, platforms, etc.)
local function collect_surfaces()
  local surfaces = {}
  for _, surface in pairs(game.surfaces) do
    surfaces[#surfaces + 1] = surface.name
  end
  return surfaces
end

--- Determine the active surface for the first connected player.
local function active_surface()
  local players = game.connected_players
  if #players > 0 then
    return players[1].surface.name
  end
  return "nauvis"
end

--- Collect game_overview section data.
local function collect_game_overview()
  local force = game.forces["player"]
  local ticks = game.ticks_played
  local hours = ticks / (60 * 60 * 60) -- 60 ticks/sec * 60 sec/min * 60 min/hr

  return {
    save_id = ensure_save_id(),
    game_version = script.active_mods["base"] or "unknown",
    ticks_played = ticks,
    hours_played = math.floor(hours * 100) / 100, -- 2 decimal places
    difficulty_settings = {
      recipe_difficulty = game.difficulty_settings.recipe_difficulty,
      technology_difficulty = game.difficulty_settings.technology_difficulty,
    },
    mods = collect_mods(),
    rocket_launches = force and force.rockets_launched or 0,
    surfaces = collect_surfaces(),
    active_surface = active_surface(),
  }
end

--- Build the full export payload matching Savecraft's expected structure.
local function build_export()
  local overview = collect_game_overview()

  local summary = string.format(
    "Factorio — %.1f hours, %d rockets launched",
    overview.hours_played,
    overview.rocket_launches
  )

  return {
    identity = {
      save_name = overview.save_id,
      game_id = "factorio",
    },
    summary = summary,
    sections = {
      game_overview = {
        description = "Map identity and high-level game state",
        data = overview,
      },
    },
  }
end

--- Write the export to disk.
local function export_state()
  local payload = build_export()
  local json = helpers.table_to_json(payload)
  helpers.write_file(EXPORT_PATH, json .. "\n", false)
end

-- Export on a regular tick interval
script.on_nth_tick(EXPORT_INTERVAL, export_state)

-- Export immediately on key game events
script.on_event(defines.events.on_research_finished, export_state)
script.on_event(defines.events.on_rocket_launched, export_state)

-- Generate save ID on new map or first load
script.on_init(function()
  ensure_save_id()
  export_state()
end)

script.on_load(function()
  -- storage is restored from save automatically; no action needed
end)
