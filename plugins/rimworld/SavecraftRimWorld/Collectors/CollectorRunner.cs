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
        const int MaxSections = 50;

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
                        if (sections.Count >= MaxSections)
                        {
                            Log.Warning($"[Savecraft] Section cap ({MaxSections}) reached, skipping remaining dynamic sections.");
                            break;
                        }
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
                if (sections.Count >= MaxSections) break;
            }

            if (sections.Count == 0)
            {
                Log.Warning("[Savecraft] No sections collected, skipping push.");
                return null;
            }

            var colonyName = Find.CurrentMap?.info?.parent?.Label
                ?? Find.World?.info?.name
                ?? "Unknown Colony";
            var ctx = ColonyContext.Capture();
            var identity = BuildIdentity(colonyName, ctx);
            var summary = BuildSummary(colonyName, ctx);

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

        /// <summary>
        /// Shared game state snapshot used by both BuildIdentity and BuildSummary,
        /// avoiding duplicate lookups of the same properties.
        /// </summary>
        struct ColonyContext
        {
            public Map Map;
            public int TicksGame;
            public float Longitude;
            public int ColonistCount;
            public double Wealth;
            public int Year;
            public string Quadrum;
            public int Day;
            public string Storyteller;
            public string Difficulty;

            public static ColonyContext Capture()
            {
                var map = Find.CurrentMap;
                var ticksGame = Find.TickManager?.TicksGame ?? 0;
                var longitude = map?.Tile != null
                    ? Find.WorldGrid.LongLatOf(map.Tile).x
                    : 0f;

                return new ColonyContext
                {
                    Map = map,
                    TicksGame = ticksGame,
                    Longitude = longitude,
                    ColonistCount = map?.mapPawns?.FreeColonistsCount ?? 0,
                    Wealth = Math.Round(map?.wealthWatcher?.WealthTotal ?? 0),
                    Year = GenDate.Year(ticksGame, longitude),
                    Quadrum = GenDate.Quadrum(ticksGame, longitude).Label(),
                    Day = GenDate.DayOfQuadrum(ticksGame, longitude) + 1,
                    Storyteller = Find.Storyteller?.def?.label ?? "Unknown",
                    Difficulty = GetDifficultyLabel() ?? "Unknown"
                };
            }
        }

        SaveIdentity BuildIdentity(string colonyName, ColonyContext ctx)
        {
            var extra = StructHelper.NewStruct();
            extra.Set("seed", Find.World?.info?.seedString ?? "");
            extra.Set("storyteller", ctx.Storyteller);
            extra.Set("difficulty", ctx.Difficulty);
            extra.Set("year", ctx.Year);
            extra.Set("quadrum", ctx.Quadrum);
            extra.Set("day", ctx.Day);
            extra.Set("colonist_count", ctx.ColonistCount);
            extra.Set("colony_wealth", ctx.Wealth);

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
        internal static string GetDifficultyLabel()
        {
            object difficulty = Find.Storyteller?.difficulty;
            if (difficulty == null) return "Unknown";
            if (difficulty is Def def) return def.label;
            return difficulty.ToString();
        }

        string BuildSummary(string colonyName, ColonyContext ctx)
        {
            return $"{colonyName} — {ctx.ColonistCount} colonists, Year {ctx.Year} {ctx.Quadrum}, {ctx.Storyteller} {ctx.Difficulty} — Colony Wealth {ctx.Wealth:N0}";
        }
    }
}
