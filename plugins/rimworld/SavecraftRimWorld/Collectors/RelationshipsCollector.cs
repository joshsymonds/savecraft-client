using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects the colony social graph as a single section.
    /// Answers: "Who is bonded or feuding?", "Who is related to whom?",
    /// "What does X think of Y?"
    /// </summary>
    public class RelationshipsCollector : ICollector
    {
        public string SectionName => "relationships";

        public string Description =>
            "Colony social graph: direct relations (lover/spouse/kin) and the full " +
            "colonist-to-colonist opinion matrix. Answers who is bonded, feuding, or related.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var colonists = Find.CurrentMap.mapPawns.FreeColonists.ToList();

            var directRelations = new List<Struct>();
            foreach (var pawn in colonists)
            {
                var relations = pawn.relations?.DirectRelations;
                if (relations == null) continue;

                foreach (var rel in relations)
                {
                    var entry = StructHelper.NewStruct();
                    entry.Set("pawn", pawn.Name?.ToStringShort ?? "Unknown");
                    entry.Set("target", rel.otherPawn?.Name?.ToStringShort ?? rel.otherPawn?.LabelShort ?? "Unknown");
                    entry.Set("relation", rel.def.label);
                    directRelations.Add(entry);
                }
            }
            s.SetList("direct_relations", directRelations);

            var opinions = new List<Struct>();
            foreach (var pawn in colonists)
            {
                if (pawn.relations == null) continue;

                foreach (var other in colonists)
                {
                    if (other == pawn) continue;

                    var entry = StructHelper.NewStruct();
                    entry.Set("pawn", pawn.Name?.ToStringShort ?? "Unknown");
                    entry.Set("target", other.Name?.ToStringShort ?? "Unknown");
                    entry.Set("opinion", pawn.relations.OpinionOf(other));
                    opinions.Add(entry);
                }
            }
            s.SetList("opinion_of", opinions);

            return s;
        }
    }
}
