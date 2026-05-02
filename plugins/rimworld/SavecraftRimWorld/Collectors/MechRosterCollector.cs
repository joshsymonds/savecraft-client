using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Summary of all player-controlled mechs.
    /// Answers: "What mechs do I have?", "Which are damaged?",
    /// "Who controls each mech?", "What's their work mode?"
    /// </summary>
    public class MechRosterCollector : ICollector
    {
        public string SectionName => "mechs";

        public string Description =>
            "Summary of all player-controlled mechs. " +
            "Per mech: name, kind, weight class, HP %, energy %, " +
            "work mode (work/escort/recharge), current job, overseer (mechanitor name).";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var mechs = new List<Struct>();

            foreach (var pawn in Find.CurrentMap.mapPawns.SpawnedPawnsInFaction(Faction.OfPlayer))
            {
                if (pawn.RaceProps?.IsMechanoid != true) continue;
                mechs.Add(CollectMech(pawn));
            }

            s.SetList("mechs", mechs);
            s.Set("count", mechs.Count);
            return s;
        }

        Struct CollectMech(Pawn pawn)
        {
            var m = StructHelper.NewStruct();

            m.Set("name", pawn.Name?.ToStringShort ?? pawn.LabelShort ?? "Unknown");
            m.Set("kind", pawn.def?.label ?? "unknown");
            m.Set("weight_class", pawn.def?.race?.mechWeightClass.ToString()?.ToLower() ?? "Unknown");

            if (pawn.health?.summaryHealth != null)
            {
                m.Set("hp_pct", System.Math.Round(pawn.health.summaryHealth.SummaryHealthPercent * 100));
            }

            var energy = pawn.needs?.TryGetNeed<Need_MechEnergy>();
            if (energy != null)
            {
                m.Set("energy_pct", System.Math.Round(energy.CurLevel * 100));
            }

            m.Set("work_mode", pawn.GetMechWorkMode()?.label ?? "Unassigned");
            m.Set("current_job", pawn.CurJob?.def?.label ?? "Idle");

            var overseer = pawn.GetOverseer();
            if (overseer != null)
            {
                m.Set("overseer", overseer.Name?.ToStringShort ?? overseer.LabelShort ?? "Unknown");
            }

            return m;
        }
    }
}
