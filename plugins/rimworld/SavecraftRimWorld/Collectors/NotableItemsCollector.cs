using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects high-value and notable items.
    /// Answers: "What are my best items?", "Do I have any masterwork gear?",
    /// "Where are my artifacts?", "What legendary items do I own?"
    /// </summary>
    public class NotableItemsCollector : ICollector
    {
        public string SectionName => "notable_items";

        public string Description =>
            "High-value items and their locations. " +
            "Items above Normal quality: item name, quality, location " +
            "(equipped by colonist / stockpile / ground).";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;
            var items = new List<Struct>();

            // Scan all things on map for quality items
            foreach (var thing in map.listerThings.AllThings)
            {
                var qualityComp = thing.TryGetComp<CompQuality>();
                if (qualityComp == null) continue;

                var quality = qualityComp.Quality;
                if (quality <= QualityCategory.Good) continue; // Only Excellent+

                var item = StructHelper.NewStruct();
                item.Set("name", thing.LabelCapNoCount);
                item.Set("quality", quality.GetLabel());

                // Location
                if (thing.ParentHolder is Pawn_EquipmentTracker eq)
                    item.Set("location", $"equipped by {eq.pawn.Name?.ToStringShort ?? "Unknown"}");
                else if (thing.ParentHolder is Pawn_ApparelTracker ap)
                    item.Set("location", $"worn by {ap.pawn.Name?.ToStringShort ?? "Unknown"}");
                else if (thing.ParentHolder is Pawn_InventoryTracker inv)
                    item.Set("location", $"carried by {inv.pawn.Name?.ToStringShort ?? "Unknown"}");
                else
                    item.Set("location", "stockpile");

                items.Add(item);
            }

            // Also check equipped items on colonists
            foreach (var pawn in map.mapPawns.FreeColonists)
            {
                // Equipment (weapons)
                if (pawn.equipment != null)
                {
                    foreach (var eq in pawn.equipment.AllEquipmentListForReading)
                    {
                        var qc = eq.TryGetComp<CompQuality>();
                        if (qc == null || qc.Quality <= QualityCategory.Good) continue;

                        // Check if already added from AllThings
                        if (eq.Spawned) continue;

                        var item = StructHelper.NewStruct();
                        item.Set("name", eq.LabelCapNoCount);
                        item.Set("quality", qc.Quality.GetLabel());
                        item.Set("location", $"equipped by {pawn.Name?.ToStringShort ?? "Unknown"}");
                        items.Add(item);
                    }
                }

                // Apparel
                if (pawn.apparel != null)
                {
                    foreach (var ap in pawn.apparel.WornApparel)
                    {
                        var qc = ap.TryGetComp<CompQuality>();
                        if (qc == null || qc.Quality <= QualityCategory.Good) continue;

                        if (ap.Spawned) continue;

                        var item = StructHelper.NewStruct();
                        item.Set("name", ap.LabelCapNoCount);
                        item.Set("quality", qc.Quality.GetLabel());
                        item.Set("location", $"worn by {pawn.Name?.ToStringShort ?? "Unknown"}");
                        items.Add(item);
                    }
                }
            }

            // Sort by quality descending
            var sorted = items
                .OrderByDescending(i =>
                {
                    var q = i.Fields.ContainsKey("quality") ? i.Fields["quality"].StringValue : "";
                    return q switch
                    {
                        "legendary" => 6,
                        "masterwork" => 5,
                        "excellent" => 4,
                        _ => 0
                    };
                })
                .Take(30) // Cap at 30 to keep section size reasonable
                .ToList();

            s.SetList("items", sorted);
            s.Set("count", sorted.Count);
            return s;
        }
    }
}
