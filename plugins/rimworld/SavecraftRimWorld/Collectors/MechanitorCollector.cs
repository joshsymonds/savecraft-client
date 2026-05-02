using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Summary of all mechanitors in the colony.
    /// Answers: "Who controls the mechs?", "How much bandwidth do I have left?",
    /// "Which mechs are in which control group?", "What's each group's work mode?"
    /// </summary>
    public class MechanitorCollector : ICollector
    {
        public string SectionName => "mechanitors";

        public string Description =>
            "Summary of all mechanitor colonists. " +
            "Per mechanitor: name, bandwidth used/total/remaining, " +
            "and per-control-group breakdown with work mode and assigned mech names.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var mechanitors = new List<Struct>();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                if (pawn.mechanitor == null) continue;
                mechanitors.Add(CollectMechanitor(pawn));
            }

            s.SetList("mechanitors", mechanitors);
            s.Set("count", mechanitors.Count);
            return s;
        }

        Struct CollectMechanitor(Pawn pawn)
        {
            var m = StructHelper.NewStruct();
            var tracker = pawn.mechanitor;

            m.Set("name", pawn.Name?.ToStringShort ?? pawn.LabelShort ?? "Unknown");
            m.Set("bandwidth_used", tracker.UsedBandwidth);
            m.Set("bandwidth_total", tracker.TotalBandwidth);
            m.Set("bandwidth_remaining", tracker.TotalBandwidth - tracker.UsedBandwidth);

            var groups = new List<Struct>();
            foreach (var group in tracker.controlGroups)
            {
                groups.Add(CollectControlGroup(group));
            }
            m.SetList("control_groups", groups);

            return m;
        }

        Struct CollectControlGroup(MechanitorControlGroup group)
        {
            var g = StructHelper.NewStruct();
            g.Set("index", group.Index);
            g.Set("work_mode", group.WorkMode?.label ?? "Unassigned");

            var mechNames = group.MechsForReading
                .Select(p => p.Name?.ToStringShort ?? p.LabelShort ?? "Unknown");
            g.SetList("mechs", mechNames);

            return g;
        }
    }
}
