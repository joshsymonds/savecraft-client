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

            // Turrets
            var turretCounts = new Dictionary<string, int>();
            foreach (var building in map.listerBuildings.allBuildingsColonist)
            {
                if (building.def.building != null && building.def.building.turretGunDef != null)
                {
                    var label = building.def.label;
                    if (turretCounts.ContainsKey(label))
                        turretCounts[label]++;
                    else
                        turretCounts[label] = 1;
                }
            }

            var turrets = new List<Struct>();
            foreach (var kv in turretCounts.OrderByDescending(kv => kv.Value))
            {
                var t = StructHelper.NewStruct();
                t.Set("type", kv.Key);
                t.Set("count", kv.Value);
                turrets.Add(t);
            }
            s.SetList("turrets", turrets);
            s.Set("turret_total", turretCounts.Values.Sum());

            // Traps
            var trapCounts = new Dictionary<string, int>();
            foreach (var building in map.listerBuildings.allBuildingsColonist)
            {
                if (building is Building_Trap)
                {
                    var label = building.def.label;
                    if (trapCounts.ContainsKey(label))
                        trapCounts[label]++;
                    else
                        trapCounts[label] = 1;
                }
            }

            var traps = new List<Struct>();
            foreach (var kv in trapCounts.OrderByDescending(kv => kv.Value))
            {
                var t = StructHelper.NewStruct();
                t.Set("type", kv.Key);
                t.Set("count", kv.Value);
                traps.Add(t);
            }
            s.SetList("traps", traps);
            s.Set("trap_total", trapCounts.Values.Sum());

            // Walls by material
            var wallCounts = new Dictionary<string, int>();
            foreach (var building in map.listerBuildings.allBuildingsColonist)
            {
                if (building.def == ThingDefOf.Wall)
                {
                    var stuffLabel = building.Stuff?.label ?? "unknown";
                    if (wallCounts.ContainsKey(stuffLabel))
                        wallCounts[stuffLabel]++;
                    else
                        wallCounts[stuffLabel] = 1;
                }
            }

            var walls = new List<Struct>();
            foreach (var kv in wallCounts.OrderByDescending(kv => kv.Value))
            {
                var w = StructHelper.NewStruct();
                w.Set("material", kv.Key);
                w.Set("count", kv.Value);
                walls.Add(w);
            }
            s.SetList("walls", walls);

            return s;
        }
    }
}
