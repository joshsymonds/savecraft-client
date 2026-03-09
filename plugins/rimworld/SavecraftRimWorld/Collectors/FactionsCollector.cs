using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects diplomatic relations data.
    /// Answers: "Who are my allies?", "Which factions are hostile?",
    /// "What's my goodwill with the Empire?", "Can I call for help?"
    /// </summary>
    public class FactionsCollector : ICollector
    {
        public string SectionName => "factions";

        public string Description =>
            "Diplomatic relations. " +
            "Per faction: name, type (pirate/tribal/outlander/empire/mechanoid), " +
            "goodwill value, hostile flag.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var factions = new List<Struct>();

            foreach (var faction in Find.FactionManager.AllFactions)
            {
                if (faction.IsPlayer) continue;
                if (faction.def.hidden) continue;

                var f = StructHelper.NewStruct();
                f.Set("name", faction.Name);
                f.Set("type", faction.def.label);
                f.Set("goodwill", faction.PlayerGoodwill);
                f.Set("relation", faction.PlayerRelationKind.ToString());
                f.Set("defeated", faction.defeated);

                if (faction.leader != null)
                    f.Set("leader", faction.leader.Name?.ToStringShort ?? "Unknown");

                factions.Add(f);
            }

            s.SetList("factions", factions);
            s.Set("count", factions.Count);
            return s;
        }
    }
}
