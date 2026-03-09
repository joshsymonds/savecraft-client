using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects research tree state.
    /// Answers: "What should I research next?", "What have I unlocked?",
    /// "How far along is my current project?"
    /// </summary>
    public class ResearchCollector : ICollector
    {
        public string SectionName => "research";

        public string Description =>
            "Research tree state. " +
            "Current project with progress %, completed research list, " +
            "available projects (prerequisites met) categorized by tech level.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var manager = Find.ResearchManager;

            // Current project — Krafs ref assemblies don't expose currentProj field,
            // so we find the in-progress project by scanning all defs
            ResearchProjectDef current = null;
            foreach (var def in DefDatabase<ResearchProjectDef>.AllDefs)
            {
                if (!def.IsFinished && manager.GetProgress(def) > 0)
                {
                    current = def;
                    break;
                }
            }
            if (current != null)
            {
                var proj = StructHelper.NewStruct();
                proj.Set("name", current.label);
                proj.Set("progress_pct", System.Math.Round(manager.GetProgress(current) / current.baseCost * 100, 1));
                proj.Set("cost", current.baseCost);
                s.Set("current_project", proj);
            }

            // Completed research
            var completed = new List<string>();
            foreach (var def in DefDatabase<ResearchProjectDef>.AllDefs)
            {
                if (def.IsFinished)
                    completed.Add(def.label);
            }
            s.SetList("completed", completed);
            s.Set("completed_count", completed.Count);

            // Available (prerequisites met, not finished)
            var available = new List<Struct>();
            foreach (var def in DefDatabase<ResearchProjectDef>.AllDefs)
            {
                if (!def.IsFinished && def.CanStartNow)
                {
                    var a = StructHelper.NewStruct();
                    a.Set("name", def.label);
                    a.Set("cost", def.baseCost);
                    a.Set("tech_level", def.techLevel.ToString());
                    available.Add(a);
                }
            }
            s.SetList("available", available);
            s.Set("available_count", available.Count);

            return s;
        }
    }
}
