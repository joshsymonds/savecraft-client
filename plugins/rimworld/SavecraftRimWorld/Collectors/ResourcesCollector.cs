using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects stockpile inventory totals by category.
    /// Answers: "Do I have enough food?", "How much steel do I have?",
    /// "What's my medicine situation?", "Will I run out of components?"
    /// </summary>
    public class ResourcesCollector : ICollector
    {
        public string SectionName => "resources";

        public string Description =>
            "Stockpile inventory totals by category. " +
            "Includes food (raw, meals, nutrition-days estimate), medicine by type, " +
            "steel, components, advanced components, plasteel, gold, silver, " +
            "cloth, chemfuel, uranium, jade, wood, and stone blocks.";

        public Struct Collect()
        {
            var map = Find.CurrentMap;
            var rc = map.resourceCounter;
            var s = StructHelper.NewStruct();

            // Food
            var food = StructHelper.NewStruct();
            food.Set("total", rc.GetCountIn(ThingCategoryDefOf.Foods));
            // FoodMeals not in Krafs ref assemblies — look up at runtime
            var foodMeals = DefDatabase<ThingCategoryDef>.GetNamed("FoodMeals", errorOnFail: false);
            food.Set("meals", foodMeals != null ? rc.GetCountIn(foodMeals) : 0);
            food.Set("raw", rc.GetCountIn(ThingCategoryDefOf.PlantFoodRaw));
            food.Set("meat", rc.GetCountIn(ThingCategoryDefOf.MeatRaw));

            // Nutrition-days estimate
            float totalNutrition = rc.TotalHumanEdibleNutrition;
            int colonistCount = map.mapPawns.FreeColonistsCount;
            if (colonistCount > 0)
            {
                float nutritionDays = totalNutrition / (1.6f * colonistCount);
                food.Set("nutrition_days", System.Math.Round(nutritionDays, 1));
            }
            else
            {
                food.Set("nutrition_days", 0);
            }
            s.Set("food", food);

            // Medicine
            var medicine = StructHelper.NewStruct();
            medicine.Set("herbal", rc.GetCount(ThingDefOf.MedicineHerbal));
            medicine.Set("standard", rc.GetCount(ThingDefOf.MedicineIndustrial));
            medicine.Set("glitterworld", rc.GetCount(ThingDefOf.MedicineUltratech));
            s.Set("medicine", medicine);

            // Building materials
            s.Set("steel", rc.GetCount(ThingDefOf.Steel));
            s.Set("wood", rc.GetCount(ThingDefOf.WoodLog));
            s.Set("plasteel", rc.GetCount(ThingDefOf.Plasteel));
            s.Set("stone_blocks", rc.GetCountIn(ThingCategoryDefOf.StoneBlocks));

            // Components
            s.Set("components", rc.GetCount(ThingDefOf.ComponentIndustrial));
            s.Set("advanced_components", rc.GetCount(ThingDefOf.ComponentSpacer));

            // Valuables
            s.Set("gold", rc.GetCount(ThingDefOf.Gold));
            s.Set("silver", rc.GetCount(ThingDefOf.Silver));
            s.Set("jade", rc.GetCount(ThingDefOf.Jade));

            // Other
            s.Set("cloth", rc.GetCount(ThingDefOf.Cloth));
            s.Set("chemfuel", rc.GetCount(ThingDefOf.Chemfuel));
            s.Set("uranium", rc.GetCount(ThingDefOf.Uranium));

            return s;
        }
    }
}
