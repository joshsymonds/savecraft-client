using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Produces one section per player-controlled mech with full detail.
    /// Answers: "What's broken on DELT-AAA-1?", "Which mechs need repair?",
    /// "Has this mech lost any parts?", "How beat up is this Centipede?"
    /// </summary>
    public class MechDetailCollector : IMultiCollector
    {
        const string DescriptionTemplate =
            "Full detail for mech {0}. " +
            "Roster fields plus age, faction, hediffs (injuries, missing parts, conditions) " +
            "and a denormalized list of missing body parts for repair-prioritization queries.";

        public List<CollectedSection> CollectAll()
        {
            var sections = new List<CollectedSection>();

            foreach (var pawn in Find.CurrentMap.mapPawns.SpawnedPawnsInFaction(Faction.OfPlayer))
            {
                if (pawn.RaceProps?.IsMechanoid != true) continue;

                var name = pawn.Name?.ToStringShort ?? pawn.LabelShort ?? "unknown";
                sections.Add(new CollectedSection
                {
                    Name = $"mech:{name.ToLower().Replace(" ", "_")}",
                    Description = string.Format(DescriptionTemplate, name),
                    Data = CollectMech(pawn)
                });
            }

            return sections;
        }

        Struct CollectMech(Pawn pawn)
        {
            var m = StructHelper.NewStruct();

            // Roster fields (mirrors MechRosterCollector)
            m.Set("name", pawn.Name?.ToStringShort ?? pawn.LabelShort ?? "Unknown");
            m.Set("kind", pawn.def?.label ?? "unknown");
            m.Set("weight_class", pawn.def?.race?.mechWeightClass?.label?.ToLower() ?? "Unknown");

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

            // Detail-only fields
            if (pawn.ageTracker != null)
            {
                m.Set("age_days", (int)(pawn.ageTracker.AgeBiologicalTicks / 60000));
            }

            m.Set("faction", "Player");

            CollectHediffs(pawn, m);

            return m;
        }

        void CollectHediffs(Pawn pawn, Struct m)
        {
            if (pawn.health?.hediffSet == null) return;

            var hediffs = new List<Struct>();
            var missing = new List<string>();

            foreach (var hediff in pawn.health.hediffSet.hediffs)
            {
                var h = StructHelper.NewStruct();
                h.Set("label", hediff.LabelCap);

                if (hediff.Part != null)
                    h.Set("body_part", hediff.Part.Label);

                if (hediff.Severity > 0 && !(hediff is Hediff_MissingPart))
                    h.Set("severity", System.Math.Round(hediff.Severity, 2));

                h.Set("type", StructHelper.ClassifyHediff(hediff));
                hediffs.Add(h);

                if (hediff is Hediff_MissingPart && hediff.Part != null)
                    missing.Add(hediff.Part.Label);
            }

            m.SetList("hediffs", hediffs);
            m.SetList("body_parts_missing", missing);
        }
    }
}
