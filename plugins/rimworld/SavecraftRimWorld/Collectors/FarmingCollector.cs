using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects agricultural state.
    /// Answers: "Will I have enough food?", "What's growing?",
    /// "How are my crops doing?", "Is it growing season?"
    /// </summary>
    public class FarmingCollector : ICollector
    {
        public string SectionName => "farming";

        public string Description =>
            "Agricultural state. " +
            "Per growing zone: crop type, growth progress, expected yield, " +
            "soil fertility, sowing status.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;
            var zones = new List<Struct>();

            foreach (var zone in map.zoneManager.AllZones.OfType<Zone_Growing>())
            {
                var z = StructHelper.NewStruct();
                z.Set("name", zone.label);

                var plantDef = zone.GetPlantDefToGrow();
                z.Set("crop", plantDef?.label ?? "none");
                z.Set("sowing_allowed", zone.allowSow);
                z.Set("cell_count", zone.Cells.Count);

                // Scan plants in zone for growth stats
                int plantCount = 0;
                float totalGrowth = 0;
                int fullyGrown = 0;

                foreach (var cell in zone.Cells)
                {
                    var things = map.thingGrid.ThingsListAt(cell);
                    foreach (var thing in things)
                    {
                        if (thing is Plant plant && plant.sown)
                        {
                            plantCount++;
                            totalGrowth += plant.Growth;
                            if (plant.Growth >= 1f)
                                fullyGrown++;
                        }
                    }
                }

                z.Set("planted", plantCount);
                if (plantCount > 0)
                    z.Set("avg_growth_pct", System.Math.Round(totalGrowth / plantCount * 100, 1));
                z.Set("fully_grown", fullyGrown);

                // Estimated yield
                if (plantDef?.plant != null && plantCount > 0)
                {
                    z.Set("grow_days", plantDef.plant.growDays);
                }

                zones.Add(z);
            }

            s.SetList("zones", zones);
            s.Set("zone_count", zones.Count);
            return s;
        }
    }
}
