using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects colony-level Anomaly DLC state as a single section.
    /// Answers: "What monolith level are we at?", "Is study complete?",
    /// "Have we provoked the void?", "How many entities have we discovered?"
    /// Empty when the Anomaly DLC is inactive.
    /// </summary>
    public class AnomalyCollector : ICollector
    {
        public string SectionName => "anomaly";

        public string Description =>
            "Colony Anomaly state: monolith level + study progress, void provocation, " +
            "and entities discovered in the codex. Only populated when the Anomaly DLC is active.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            if (!ModsConfig.AnomalyActive) return s;

            var anomaly = Find.Anomaly;
            if (anomaly == null) return s;

            s.Set("monolith_level", anomaly.Level);
            s.Set("highest_level_reached", anomaly.HighestLevelReached);

            if (anomaly.LevelDef != null)
                s.Set("level_def", anomaly.LevelDef.label);
            else
                s.SetNull("level_def");

            s.Set("monolith_study_progress", anomaly.MonolithStudyProgress);
            s.Set("monolith_study_completed", anomaly.MonolithStudyCompleted);
            s.Set("anomaly_study_enabled", anomaly.AnomalyStudyEnabled);
            s.Set("void_provocation_performed", anomaly.hasPerformedVoidProvocation);

            var codex = Find.EntityCodex;
            int discovered = 0;
            if (codex != null)
            {
                foreach (var category in DefDatabase<EntityCategoryDef>.AllDefs)
                    discovered += codex.DiscoveredCount(category);
            }
            s.Set("entities_discovered", discovered);

            return s;
        }
    }
}
