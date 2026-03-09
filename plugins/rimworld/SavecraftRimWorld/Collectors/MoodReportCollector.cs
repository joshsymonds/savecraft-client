using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects mood status across the colony.
    /// Answers: "Who's about to break?", "What's killing morale?",
    /// "Is anyone at risk of a mental break?", "What mood buffs do we have?"
    /// </summary>
    public class MoodReportCollector : ICollector
    {
        public string SectionName => "mood_report";

        public string Description =>
            "Mood status across the colony. " +
            "Per colonist: mood value, mental break thresholds, " +
            "top mood modifiers (positive and negative), mental break risk level.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var colonists = new List<Struct>();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                var p = StructHelper.NewStruct();
                p.Set("name", pawn.Name?.ToStringShort ?? "Unknown");

                var mood = pawn.needs?.mood;
                if (mood == null)
                {
                    p.Set("mood", "N/A");
                    colonists.Add(p);
                    continue;
                }

                p.Set("mood", System.Math.Round(mood.CurLevel * 100));

                // Mental break thresholds
                var breaker = pawn.mindState?.mentalBreaker;
                if (breaker != null)
                {
                    p.Set("break_threshold_minor", System.Math.Round(breaker.BreakThresholdMinor * 100));
                    p.Set("break_threshold_major", System.Math.Round(breaker.BreakThresholdMajor * 100));
                    p.Set("break_threshold_extreme", System.Math.Round(breaker.BreakThresholdExtreme * 100));

                    // Break risk level
                    string risk;
                    if (mood.CurLevel <= breaker.BreakThresholdExtreme)
                        risk = "extreme";
                    else if (mood.CurLevel <= breaker.BreakThresholdMajor)
                        risk = "major";
                    else if (mood.CurLevel <= breaker.BreakThresholdMinor)
                        risk = "minor";
                    else
                        risk = "safe";
                    p.Set("break_risk", risk);
                }

                // Top mood modifiers (up to 5 worst and 3 best)
                var thoughts = mood.thoughts?.memories?.Memories;
                if (thoughts != null && thoughts.Count > 0)
                {
                    var negatives = new List<Struct>();
                    foreach (var t in thoughts.OrderBy(t => t.MoodOffset()).Take(5))
                    {
                        if (t.MoodOffset() >= 0) break;
                        var m = StructHelper.NewStruct();
                        m.Set("thought", t.LabelCap);
                        m.Set("offset", System.Math.Round(t.MoodOffset(), 1));
                        negatives.Add(m);
                    }
                    if (negatives.Count > 0)
                        p.SetList("worst_modifiers", negatives);

                    var positives = new List<Struct>();
                    foreach (var t in thoughts.OrderByDescending(t => t.MoodOffset()).Take(3))
                    {
                        if (t.MoodOffset() <= 0) break;
                        var m = StructHelper.NewStruct();
                        m.Set("thought", t.LabelCap);
                        m.Set("offset", System.Math.Round(t.MoodOffset(), 1));
                        positives.Add(m);
                    }
                    if (positives.Count > 0)
                        p.SetList("best_modifiers", positives);
                }

                colonists.Add(p);
            }

            s.SetList("colonists", colonists);
            s.Set("count", colonists.Count);
            return s;
        }
    }
}
