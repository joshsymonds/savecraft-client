using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects medical status across the colony.
    /// Answers: "Who needs treatment?", "How's the plague progressing?",
    /// "Who has chronic conditions?", "Is anyone bleeding out?"
    /// </summary>
    public class HealthReportCollector : ICollector
    {
        public string SectionName => "health_report";

        public string Description =>
            "Medical status across the colony. " +
            "Per colonist: hediff list (injuries, diseases, chronic conditions, implants), " +
            "immunity progress for diseases, bleeding rate, pain level, consciousness.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var colonists = new List<Struct>();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                var p = StructHelper.NewStruct();
                p.Set("name", pawn.Name?.ToStringShort ?? "Unknown");

                // Pain and consciousness
                p.Set("pain", System.Math.Round(pawn.health.hediffSet.PainTotal * 100));
                var consciousness = pawn.health.capacities.GetLevel(PawnCapacityDefOf.Consciousness);
                p.Set("consciousness", System.Math.Round(consciousness * 100));

                // Bleeding
                float bleedRate = pawn.health.hediffSet.BleedRateTotal;
                if (bleedRate > 0)
                    p.Set("bleed_rate", System.Math.Round(bleedRate * 100, 1));

                // Hediffs
                var hediffs = new List<Struct>();
                foreach (var hediff in pawn.health.hediffSet.hediffs)
                {
                    var h = StructHelper.NewStruct();
                    h.Set("label", hediff.LabelCap);

                    if (hediff.Part != null)
                        h.Set("part", hediff.Part.Label);

                    if (hediff.Severity > 0 && !(hediff is Hediff_MissingPart))
                        h.Set("severity", System.Math.Round(hediff.Severity, 2));

                    // Category
                    if (hediff is Hediff_Injury)
                        h.Set("type", "injury");
                    else if (hediff is Hediff_MissingPart)
                        h.Set("type", "missing_part");
                    else if (hediff is Hediff_AddedPart)
                        h.Set("type", "implant");
                    else if (hediff.def.lethalSeverity > 0)
                        h.Set("type", "disease");
                    else
                        h.Set("type", "condition");

                    // Immunity for diseases
                    if (hediff.def.lethalSeverity > 0)
                    {
                        var immunity = pawn.health.immunity.GetImmunity(hediff.def);
                        if (immunity > 0)
                            h.Set("immunity", System.Math.Round(immunity * 100, 1));
                    }

                    hediffs.Add(h);
                }

                if (hediffs.Count > 0)
                    p.SetList("hediffs", hediffs);
                else
                    p.Set("status", "Healthy");

                colonists.Add(p);
            }

            s.SetList("colonists", colonists);
            s.Set("count", colonists.Count);
            return s;
        }
    }
}
