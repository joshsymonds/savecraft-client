using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects a summary of all colonists for quick comparison.
    /// Answers: "Who are my colonists?", "Who's in bad shape?",
    /// "Who has the best skills?", "Who's about to break?"
    /// </summary>
    public class ColonistRosterCollector : ICollector
    {
        public string SectionName => "colonist_roster";

        public string Description =>
            "Summary of all colonists for quick comparison. " +
            "Per pawn: name, age, best skill with passion, mood value and worst modifier, " +
            "current job, and health status (healthy/injured/sick).";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var colonists = new List<Struct>();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                colonists.Add(CollectPawn(pawn));
            }

            s.SetList("colonists", colonists);
            s.Set("count", colonists.Count);
            return s;
        }

        Struct CollectPawn(Pawn pawn)
        {
            var p = StructHelper.NewStruct();

            // Name and age
            p.Set("name", pawn.Name?.ToStringShort ?? "Unknown");
            p.Set("full_name", pawn.Name?.ToStringFull ?? "Unknown");
            p.Set("age", pawn.ageTracker.AgeBiologicalYears);

            // Best skill
            var bestSkill = pawn.skills?.skills?
                .Where(sk => !sk.TotallyDisabled)
                .OrderByDescending(sk => sk.Level)
                .FirstOrDefault();

            if (bestSkill != null)
            {
                var passionStr = bestSkill.passion == Passion.Major ? " (major passion)"
                    : bestSkill.passion == Passion.Minor ? " (minor passion)"
                    : "";
                p.Set("best_skill", $"{bestSkill.def.label} {bestSkill.Level}{passionStr}");
            }
            else
            {
                p.Set("best_skill", "None");
            }

            // Mood
            var mood = pawn.needs?.mood;
            if (mood != null)
            {
                p.Set("mood", System.Math.Round(mood.CurLevel * 100));

                // Worst mood modifier from memory thoughts
                var worstThought = mood.thoughts?.memories?.Memories?
                    .OrderBy(t => t.MoodOffset())
                    .FirstOrDefault();
                if (worstThought != null && worstThought.MoodOffset() < 0)
                {
                    p.Set("worst_mood_modifier",
                        $"{worstThought.LabelCap} ({worstThought.MoodOffset():+0;-0})");
                }
            }

            // Current job
            p.Set("current_job", pawn.CurJob?.def?.label ?? "Idle");

            // Health status
            p.Set("health_status", GetHealthStatus(pawn));

            return p;
        }

        string GetHealthStatus(Pawn pawn)
        {
            if (pawn.health.Dead) return "Dead";
            if (pawn.health.Downed) return "Downed";

            var hediffs = pawn.health.hediffSet.hediffs;
            bool hasInjury = hediffs.Any(h => h is Hediff_Injury);
            bool hasMissingPart = hediffs.Any(h => h is Hediff_MissingPart);
            bool hasDisease = hediffs.Any(h =>
                !(h is Hediff_Injury) && !(h is Hediff_MissingPart) && !(h is Hediff_AddedPart)
                && h.def.lethalSeverity > 0);

            if (hasDisease && hasInjury) return "Sick + Injured";
            if (hasDisease) return "Sick";
            if (hasInjury) return "Injured";
            if (hasMissingPart) return "Scarred";
            return "Healthy";
        }
    }
}
