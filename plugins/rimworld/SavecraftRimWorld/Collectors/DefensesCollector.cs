using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects military infrastructure data.
    /// Answers: "Am I ready for a raid?", "How many turrets do I have?",
    /// "What's my defense setup?", "Do I need more traps?"
    /// </summary>
    public class DefensesCollector : ICollector
    {
        public string SectionName => "defenses";

        public string Description =>
            "Military infrastructure. " +
            "Turrets (type, count), traps (type, count), " +
            "wall material counts — types and quantities only.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;

            // Single pass over all colonist buildings
            var turretCounts = new Dictionary<string, int>();
            var trapCounts = new Dictionary<string, int>();
            var wallCounts = new Dictionary<string, int>();

            foreach (var building in map.listerBuildings.allBuildingsColonist)
            {
                if (building.def.building != null && building.def.building.IsTurret)
                    turretCounts.Increment(building.def.label);
                else if (building is Building_Trap)
                    trapCounts.Increment(building.def.label);
                else if (building.def == ThingDefOf.Wall)
                    wallCounts.Increment(building.Stuff?.label ?? "unknown");
            }

            s.SetList("turrets", CountsToStructList(turretCounts, "type"));
            s.Set("turret_total", turretCounts.Values.Sum());
            s.SetList("traps", CountsToStructList(trapCounts, "type"));
            s.Set("trap_total", trapCounts.Values.Sum());
            s.SetList("walls", CountsToStructList(wallCounts, "material"));

            return s;
        }

        static List<Struct> CountsToStructList(Dictionary<string, int> counts, string keyName)
        {
            var result = new List<Struct>();
            foreach (var kv in counts.OrderByDescending(kv => kv.Value))
            {
                var item = StructHelper.NewStruct();
                item.Set(keyName, kv.Key);
                item.Set("count", kv.Value);
                result.Add(item);
            }
            return result;
        }
    }
}
