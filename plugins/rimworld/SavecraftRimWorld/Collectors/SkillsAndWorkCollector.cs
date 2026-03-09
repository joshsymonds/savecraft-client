using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects work assignment optimization data.
    /// Answers: "Who should do what?", "Who has the best skill for this job?",
    /// "What work types are unassigned?", "Who's incapable of what?"
    /// </summary>
    public class SkillsAndWorkCollector : ICollector
    {
        public string SectionName => "skills_and_work";

        public string Description =>
            "Work assignment optimization data. " +
            "Per colonist: all 12 skills with passion indicator, " +
            "work priority per work type (1-4 or disabled), incapable-of flags.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var colonists = new List<Struct>();

            var workTypes = DefDatabase<WorkTypeDef>.AllDefsListForReading
                .OrderBy(w => w.naturalPriority)
                .Reverse()
                .ToList();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                var p = StructHelper.NewStruct();
                p.Set("name", pawn.Name?.ToStringShort ?? "Unknown");

                // All skills
                var skills = StructHelper.NewStruct();
                if (pawn.skills != null)
                {
                    foreach (var skill in pawn.skills.skills)
                    {
                        var passionStr = skill.passion == Passion.Major ? "!!"
                            : skill.passion == Passion.Minor ? "!"
                            : "";
                        skills.Set(skill.def.label, $"{skill.Level}{passionStr}{(skill.TotallyDisabled ? " (disabled)" : "")}");
                    }
                }
                p.Set("skills", skills);

                // Work priorities
                var work = StructHelper.NewStruct();
                if (pawn.workSettings != null)
                {
                    foreach (var wt in workTypes)
                    {
                        if (pawn.WorkTypeIsDisabled(wt))
                            work.Set(wt.label, "incapable");
                        else
                            work.Set(wt.label, pawn.workSettings.GetPriority(wt));
                    }
                }
                p.Set("work_priorities", work);

                colonists.Add(p);
            }

            s.SetList("colonists", colonists);
            s.Set("count", colonists.Count);
            return s;
        }
    }
}
