using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects electrical grid status.
    /// Answers: "Do I have enough power?", "How much battery is left?",
    /// "What's consuming the most power?", "Am I running a surplus?"
    /// </summary>
    public class PowerCollector : ICollector
    {
        public string SectionName => "power";

        public string Description =>
            "Electrical grid status. " +
            "Generators (type, output), batteries (stored/max), " +
            "total consumption, net surplus/deficit.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;

            float totalGeneration = 0;
            float totalConsumption = 0;
            float totalStored = 0;
            float totalCapacity = 0;

            var generatorCounts = new Dictionary<string, float>();
            var nets = map.powerNetManager.AllNetsListForReading;

            foreach (var net in nets)
            {
                // Generators and consumers
                foreach (var comp in net.powerComps)
                {
                    if (comp.PowerOutput > 0)
                    {
                        totalGeneration += comp.PowerOutput;
                        generatorCounts.Add(comp.parent.def.label, comp.PowerOutput);
                    }
                    else if (comp.PowerOutput < 0)
                    {
                        totalConsumption += -comp.PowerOutput;
                    }
                }

                // Batteries
                foreach (var batt in net.batteryComps)
                {
                    totalStored += batt.StoredEnergy;
                    totalCapacity += batt.Props.storedEnergyMax;
                }
            }

            s.Set("total_generation_w", System.Math.Round(totalGeneration));
            s.Set("total_consumption_w", System.Math.Round(totalConsumption));
            s.Set("net_surplus_w", System.Math.Round(totalGeneration - totalConsumption));
            s.Set("battery_stored_wd", System.Math.Round(totalStored));
            s.Set("battery_capacity_wd", System.Math.Round(totalCapacity));

            if (totalCapacity > 0)
                s.Set("battery_pct", System.Math.Round(totalStored / totalCapacity * 100));

            // Generator breakdown
            var generators = new List<Struct>();
            foreach (var kv in generatorCounts.OrderByDescending(kv => kv.Value))
            {
                var g = StructHelper.NewStruct();
                g.Set("type", kv.Key);
                g.Set("total_output_w", System.Math.Round(kv.Value));
                generators.Add(g);
            }
            s.SetList("generators", generators);
            s.Set("grid_count", nets.Count);

            return s;
        }
    }
}
