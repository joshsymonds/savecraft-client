using System;
using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Savecraft.V1;
using Verse;
using Message = Savecraft.V1.Message;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Orchestrates all registered collectors, builds the PushSave message.
    /// Must be called on Unity's main thread (collectors read live game state).
    /// </summary>
    public class CollectorRunner
    {
        readonly List<ICollector> collectors = new List<ICollector>();
        readonly List<IMultiCollector> multiCollectors = new List<IMultiCollector>();

        public void Register(ICollector collector)
        {
            collectors.Add(collector);
        }

        public void Register(IMultiCollector collector)
        {
            multiCollectors.Add(collector);
        }

        /// <summary>
        /// Run all collectors and build a complete PushSave message.
        /// Called on main thread after a save event.
        /// </summary>
        public Message BuildPushSave()
        {
            var map = Find.CurrentMap;
            if (map == null)
            {
                Log.Warning("[Savecraft] No current map, skipping push.");
                return null;
            }

            var sections = new List<GameSection>();

            foreach (var collector in collectors)
            {
                try
                {
                    var data = collector.Collect();
                    sections.Add(new GameSection
                    {
                        Name = collector.SectionName,
                        Description = collector.Description,
                        Data = data
                    });
                }
                catch (Exception ex)
                {
                    Log.Error($"[Savecraft] Collector '{collector.SectionName}' failed: {ex}");
                }
            }

            foreach (var multi in multiCollectors)
            {
                try
                {
                    foreach (var cs in multi.CollectAll())
                    {
                        sections.Add(new GameSection
                        {
                            Name = cs.Name,
                            Description = cs.Description,
                            Data = cs.Data
                        });
                    }
                }
                catch (Exception ex)
                {
                    Log.Error($"[Savecraft] Multi-collector failed: {ex}");
                }
            }

            if (sections.Count == 0)
            {
                Log.Warning("[Savecraft] No sections collected, skipping push.");
                return null;
            }

            var colonyName = Find.CurrentMap?.info?.parent?.Label
                ?? Find.World?.info?.name
                ?? "Unknown Colony";
            var identity = BuildIdentity(colonyName);
            var summary = BuildSummary(colonyName);

            var pushSave = new PushSave
            {
                Identity = identity,
                Summary = summary,
                ParsedAt = Timestamp.FromDateTime(DateTime.UtcNow),
                GameId = "rimworld"
            };
            pushSave.Sections.AddRange(sections);

            return new Message { PushSave = pushSave };
        }

        SaveIdentity BuildIdentity(string colonyName)
        {
            var extra = StructHelper.NewStruct();
            extra.Set("seed", Find.World?.info?.seedString ?? "");
            extra.Set("storyteller", Find.Storyteller?.def?.label ?? "");
            extra.Set("difficulty", GetDifficultyLabel() ?? "");

            var ticksGame = Find.TickManager?.TicksGame ?? 0;
            var longitude = Find.CurrentMap?.Tile != null
                ? Find.WorldGrid.LongLatOf(Find.CurrentMap.Tile).x
                : 0f;

            extra.Set("year", GenDate.Year(ticksGame, longitude));
            extra.Set("quadrum", GenDate.Quadrum(ticksGame, longitude).Label());
            extra.Set("day", GenDate.DayOfQuadrum(ticksGame, longitude) + 1);

            var map = Find.CurrentMap;
            extra.Set("colonist_count", map?.mapPawns?.FreeColonistsCount ?? 0);
            extra.Set("colony_wealth", Math.Round(map?.wealthWatcher?.WealthTotal ?? 0));

            return new SaveIdentity
            {
                Name = colonyName,
                Extra = extra
            };
        }

        /// <summary>
        /// Get the difficulty label. The Krafs ref assemblies type Storyteller.difficulty as
        /// Difficulty (settings class) instead of DifficultyDef (the actual runtime type, a Def
        /// with .label). We cast at runtime to get the label safely.
        /// </summary>
        static string GetDifficultyLabel()
        {
            object difficulty = Find.Storyteller?.difficulty;
            if (difficulty == null) return "Unknown";
            // At runtime, Storyteller.difficulty is DifficultyDef (a Def subclass with .label).
            // The Krafs ref assemblies mis-type it as Difficulty (the settings class).
            // Cast through object to bypass compile-time type checking.
            if (difficulty is Def def) return def.label;
            return difficulty.ToString();
        }

        string BuildSummary(string colonyName)
        {
            var map = Find.CurrentMap;
            var colonistCount = map?.mapPawns?.FreeColonistsCount ?? 0;
            var ticksGame = Find.TickManager?.TicksGame ?? 0;
            var longitude = map?.Tile != null ? Find.WorldGrid.LongLatOf(map.Tile).x : 0f;
            var year = GenDate.Year(ticksGame, longitude);
            var quadrum = GenDate.Quadrum(ticksGame, longitude).Label();
            var storyteller = Find.Storyteller?.def?.label ?? "Unknown";
            var difficulty = GetDifficultyLabel() ?? "Unknown";
            var wealth = Math.Round(map?.wealthWatcher?.WealthTotal ?? 0);

            return $"{colonyName} — {colonistCount} colonists, Year {year} {quadrum}, {storyteller} {difficulty} — Colony Wealth {wealth:N0}";
        }
    }
}
