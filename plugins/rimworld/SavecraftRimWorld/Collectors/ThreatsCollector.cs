using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects threat context for raid readiness.
    /// Answers: "How big will the next raid be?", "Why are raids so hard?",
    /// "What's driving raid scaling?", "What happened recently?"
    /// </summary>
    public class ThreatsCollector : ICollector
    {
        public string SectionName => "threats";

        public string Description =>
            "Threat context for raid readiness. " +
            "Colony wealth points, threat scaling factors, " +
            "recent major incidents (last 5 with type and date).";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;

            // Wealth drives raid points
            s.Set("colony_wealth", System.Math.Round(map.wealthWatcher.WealthTotal));
            s.Set("colonist_count", map.mapPawns.FreeColonistsCount);

            // Storyteller info
            s.Set("storyteller", Find.Storyteller.def.label);
            s.Set("difficulty", CollectorRunner.GetDifficultyLabel());

            // Adaptation factor (affects raid strength scaling)
            s.Set("adaptation", System.Math.Round(Find.StoryWatcher.watcherAdaptation.AdaptDays, 1));

            // Total major threats experienced
            s.Set("total_threats", Find.StoryWatcher.statsRecord.numThreatBigs);

            // Combat-capable colonists
            int combatReady = 0;
            foreach (var pawn in map.mapPawns.FreeColonists)
            {
                if (!pawn.Downed && !pawn.InMentalState && pawn.health.capacities.CapableOf(PawnCapacityDefOf.Manipulation))
                    combatReady++;
            }
            s.Set("combat_ready_colonists", combatReady);

            return s;
        }
    }
}
