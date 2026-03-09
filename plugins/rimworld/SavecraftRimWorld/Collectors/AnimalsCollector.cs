using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects colony animals and livestock data.
    /// Answers: "What animals do I have?", "Who's bonded?",
    /// "What training do my animals have?", "Any pregnant animals?"
    /// </summary>
    public class AnimalsCollector : ICollector
    {
        public string SectionName => "animals";

        public string Description =>
            "Colony animals and livestock. " +
            "Per animal: species, name (if named), bonded colonist, " +
            "training (obedience/release/rescue/haul), pregnancy status.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;
            var animals = new List<Struct>();

            // Hoist def lookups outside loop
            var rescueDef = DefDatabase<TrainableDef>.GetNamed("Rescue", errorOnFail: false);
            var haulDef = DefDatabase<TrainableDef>.GetNamed("Haul", errorOnFail: false);

            var colonyAnimals = map.mapPawns.SpawnedPawnsInFaction(Faction.OfPlayer)
                .Where(p => p.RaceProps.Animal);

            // Group by species for compactness
            var bySpecies = new Dictionary<string, List<Pawn>>();
            foreach (var animal in colonyAnimals)
            {
                var species = animal.def.label;
                if (!bySpecies.ContainsKey(species))
                    bySpecies[species] = new List<Pawn>();
                bySpecies[species].Add(animal);
            }

            foreach (var kv in bySpecies.OrderByDescending(kv => kv.Value.Count))
            {
                // For species with many unnamed animals, summarize
                if (kv.Value.Count > 3 && kv.Value.All(a => a.Name == null))
                {
                    var summary = StructHelper.NewStruct();
                    summary.Set("species", kv.Key);
                    summary.Set("count", kv.Value.Count);

                    // Check for any pregnant
                    int pregnant = kv.Value.Count(a =>
                        a.health.hediffSet.GetFirstHediffOfDef(HediffDefOf.Pregnant) != null);
                    if (pregnant > 0)
                        summary.Set("pregnant", pregnant);

                    animals.Add(summary);
                    continue;
                }

                // Individual animals
                foreach (var animal in kv.Value)
                {
                    var a = StructHelper.NewStruct();
                    a.Set("species", kv.Key);

                    if (animal.Name != null)
                        a.Set("name", animal.Name.ToStringShort);

                    // Bonded colonist
                    if (animal.relations != null)
                    {
                        foreach (var rel in animal.relations.DirectRelations)
                        {
                            if (rel.def == PawnRelationDefOf.Bond && !rel.otherPawn.RaceProps.Animal)
                            {
                                a.Set("bonded_to", rel.otherPawn.Name?.ToStringShort ?? "Unknown");
                                break;
                            }
                        }
                    }

                    // Training
                    if (animal.training != null)
                    {
                        var training = new List<string>();
                        if (animal.training.HasLearned(TrainableDefOf.Obedience))
                            training.Add("obedience");
                        if (animal.training.HasLearned(TrainableDefOf.Release))
                            training.Add("release");

                        if (rescueDef != null && animal.training.HasLearned(rescueDef))
                            training.Add("rescue");

                        if (haulDef != null && animal.training.HasLearned(haulDef))
                            training.Add("haul");

                        if (training.Count > 0)
                            a.SetList("training", training);
                    }

                    // Pregnancy
                    var pregnancy = animal.health.hediffSet.GetFirstHediffOfDef(HediffDefOf.Pregnant);
                    if (pregnancy != null)
                        a.Set("pregnant", true);

                    animals.Add(a);
                }
            }

            s.SetList("animals", animals);

            int totalCount = bySpecies.Values.Sum(list => list.Count);
            s.Set("total_count", totalCount);
            s.Set("species_count", bySpecies.Count);

            return s;
        }
    }
}
