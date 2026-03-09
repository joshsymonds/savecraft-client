using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects colony-level overview data: identity, global stats, game settings.
    /// Answers: "What kind of colony is this?", "What storyteller/difficulty?",
    /// "How wealthy is the colony?", "What DLCs/mods are active?"
    /// </summary>
    public class ColonyOverviewCollector : ICollector
    {
        public string SectionName => "colony_overview";

        public string Description =>
            "Colony identity, global stats, and game settings. " +
            "Answers questions about colony name, biome, storyteller, difficulty, date, " +
            "wealth breakdown, colonist/prisoner/animal counts, active DLCs, and mod count.";

        public Struct Collect()
        {
            var map = Find.CurrentMap;
            var ticksGame = Find.TickManager.TicksGame;
            var longitude = Find.WorldGrid.LongLatOf(map.Tile).x;

            var s = StructHelper.NewStruct();

            // Colony identity
            s.Set("colony_name", map.info?.parent?.Label ?? "Unknown");
            s.Set("seed", Find.World.info.seedString);
            s.Set("biome", map.Biome.label);

            // Game settings
            s.Set("storyteller", Find.Storyteller.def.label);
            // Krafs ref assemblies mis-type Storyteller.difficulty as Difficulty (settings class)
            // instead of DifficultyDef (Def subclass). Cast through object for runtime resolution.
            object difficulty = Find.Storyteller.difficulty;
            s.Set("difficulty", difficulty is Def diffDef ? diffDef.label : difficulty.ToString());
            s.Set("permadeath", Find.GameInfo.permadeathMode);

            // Date
            s.Set("year", GenDate.Year(ticksGame, longitude));
            s.Set("quadrum", GenDate.Quadrum(ticksGame, longitude).Label());
            s.Set("day", GenDate.DayOfQuadrum(ticksGame, longitude) + 1);

            // Wealth breakdown
            var wealth = StructHelper.NewStruct();
            wealth.Set("total", System.Math.Round(map.wealthWatcher.WealthTotal));
            wealth.Set("items", System.Math.Round(map.wealthWatcher.WealthItems));
            wealth.Set("buildings", System.Math.Round(map.wealthWatcher.WealthBuildings));
            wealth.Set("pawns", System.Math.Round(map.wealthWatcher.WealthPawns));
            wealth.Set("floors", System.Math.Round(map.wealthWatcher.WealthFloorsOnly));
            s.Set("wealth", wealth);

            // Pawn counts
            s.Set("colonist_count", map.mapPawns.FreeColonistsCount);
            s.Set("prisoner_count", map.mapPawns.PrisonersOfColonyCount);

            var animalCount = map.mapPawns.SpawnedPawnsInFaction(Faction.OfPlayer)
                .Count(p => p.RaceProps.Animal);
            s.Set("animal_count", animalCount);

            // Active DLCs
            var dlcs = new List<string>();
            if (ModsConfig.RoyaltyActive) dlcs.Add("Royalty");
            if (ModsConfig.IdeologyActive) dlcs.Add("Ideology");
            if (ModsConfig.BiotechActive) dlcs.Add("Biotech");
            if (ModsConfig.AnomalyActive) dlcs.Add("Anomaly");
            s.SetList("active_dlcs", dlcs);

            // Mod count (excluding core + DLCs)
            var modCount = ModsConfig.ActiveModsInLoadOrder
                .Count(m => !m.Official);
            s.Set("active_mod_count", modCount);

            return s;
        }
    }
}
