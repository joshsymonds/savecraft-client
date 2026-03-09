using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Produces one section per colonist with full detail.
    /// Answers: "Tell me everything about Engie", "Why is she upset?",
    /// "What gear does she have?", "What are her backstories and traits?"
    /// </summary>
    public class ColonistDetailCollector : IMultiCollector
    {
        const string DescriptionTemplate =
            "Full detail for colonist {0}. " +
            "Backstory, all traits, all skills (level + passion), " +
            "mood value + all modifiers, all hediffs, equipment + apparel (with quality), " +
            "needs breakdown, current job, schedule.";

        public List<CollectedSection> CollectAll()
        {
            var sections = new List<CollectedSection>();

            foreach (var pawn in Find.CurrentMap.mapPawns.FreeColonists)
            {
                var name = pawn.Name?.ToStringShort ?? "Unknown";
                sections.Add(new CollectedSection
                {
                    Name = $"colonist:{name.ToLower().Replace(" ", "_")}",
                    Description = string.Format(DescriptionTemplate, name),
                    Data = CollectPawn(pawn)
                });
            }

            return sections;
        }

        Struct CollectPawn(Pawn pawn)
        {
            var p = StructHelper.NewStruct();
            var name = pawn.Name?.ToStringShort ?? "Unknown";

            p.Set("name", name);
            p.Set("full_name", pawn.Name?.ToStringFull ?? "Unknown");
            p.Set("age", pawn.ageTracker.AgeBiologicalYears);
            p.Set("gender", pawn.gender.ToString());

            CollectBackstory(pawn, p);
            CollectTraits(pawn, p);
            CollectSkills(pawn, p);
            CollectMood(pawn, p);
            CollectHealth(pawn, p);
            CollectEquipment(pawn, p);
            CollectNeeds(pawn, p);

            p.Set("current_job", pawn.CurJob?.def?.label ?? "Idle");
            CollectSchedule(pawn, p);

            return p;
        }

        void CollectBackstory(Pawn pawn, Struct p)
        {
            if (pawn.story == null) return;

            var childhood = pawn.story.GetBackstory(BackstorySlot.Childhood);
            if (childhood != null)
                p.Set("childhood", childhood.title);

            var adulthood = pawn.story.GetBackstory(BackstorySlot.Adulthood);
            if (adulthood != null)
                p.Set("adulthood", adulthood.title);
        }

        void CollectTraits(Pawn pawn, Struct p)
        {
            if (pawn.story?.traits == null) return;

            var traits = new List<string>();
            foreach (var trait in pawn.story.traits.allTraits)
            {
                traits.Add(trait.LabelCap);
            }
            if (traits.Count > 0)
                p.SetList("traits", traits);
        }

        void CollectSkills(Pawn pawn, Struct p)
        {
            if (pawn.skills == null) return;

            var skills = StructHelper.NewStruct();
            foreach (var skill in pawn.skills.skills)
            {
                var passionStr = skill.passion == Passion.Major ? "!!"
                    : skill.passion == Passion.Minor ? "!"
                    : "";
                var disabled = skill.TotallyDisabled ? " (disabled)" : "";
                skills.Set(skill.def.label, $"{skill.Level}{passionStr}{disabled}");
            }
            p.Set("skills", skills);
        }

        void CollectMood(Pawn pawn, Struct p)
        {
            var mood = pawn.needs?.mood;
            if (mood == null) return;

            p.Set("mood", System.Math.Round(mood.CurLevel * 100));

            // All thought modifiers
            var thoughts = mood.thoughts?.memories?.Memories;
            if (thoughts == null || thoughts.Count == 0) return;

            var modifiers = new List<Struct>();
            foreach (var t in thoughts.OrderBy(t => t.MoodOffset()))
            {
                float offset = t.MoodOffset();
                if (System.Math.Abs(offset) < 0.1f) continue;

                var m = StructHelper.NewStruct();
                m.Set("thought", t.LabelCap);
                m.Set("offset", System.Math.Round(offset, 1));
                modifiers.Add(m);
            }
            if (modifiers.Count > 0)
                p.SetList("mood_modifiers", modifiers);
        }

        void CollectHealth(Pawn pawn, Struct p)
        {
            var hediffs = pawn.health.hediffSet.hediffs;
            if (hediffs.Count == 0)
            {
                p.Set("health", "Healthy");
                return;
            }

            var healthList = new List<Struct>();
            foreach (var hediff in hediffs)
            {
                var h = StructHelper.NewStruct();
                h.Set("label", hediff.LabelCap);

                if (hediff.Part != null)
                    h.Set("part", hediff.Part.Label);

                if (hediff.Severity > 0 && !(hediff is Hediff_MissingPart))
                    h.Set("severity", System.Math.Round(hediff.Severity, 2));

                if (hediff is Hediff_Injury) h.Set("type", "injury");
                else if (hediff is Hediff_MissingPart) h.Set("type", "missing_part");
                else if (hediff is Hediff_AddedPart) h.Set("type", "implant");
                else if (hediff.def.lethalSeverity > 0) h.Set("type", "disease");
                else h.Set("type", "condition");

                healthList.Add(h);
            }
            p.SetList("health", healthList);
        }

        void CollectEquipment(Pawn pawn, Struct p)
        {
            // Primary weapon
            var primary = pawn.equipment?.Primary;
            if (primary != null)
            {
                var weapon = StructHelper.NewStruct();
                weapon.Set("name", primary.LabelCap);
                var qc = primary.TryGetComp<CompQuality>();
                if (qc != null)
                    weapon.Set("quality", qc.Quality.GetLabel());
                p.Set("weapon", weapon);
            }

            // Apparel
            var worn = pawn.apparel?.WornApparel;
            if (worn != null && worn.Count > 0)
            {
                var apparel = new List<Struct>();
                foreach (var ap in worn)
                {
                    var a = StructHelper.NewStruct();
                    a.Set("name", ap.LabelCap);
                    var qc = ap.TryGetComp<CompQuality>();
                    if (qc != null)
                        a.Set("quality", qc.Quality.GetLabel());
                    apparel.Add(a);
                }
                p.SetList("apparel", apparel);
            }
        }

        void CollectNeeds(Pawn pawn, Struct p)
        {
            var allNeeds = pawn.needs?.AllNeeds;
            if (allNeeds == null) return;

            var needs = StructHelper.NewStruct();
            foreach (var need in allNeeds)
            {
                needs.Set(need.def.label, System.Math.Round(need.CurLevelPercentage * 100));
            }
            p.Set("needs", needs);
        }

        void CollectSchedule(Pawn pawn, Struct p)
        {
            var timetable = pawn.timetable;
            if (timetable?.times == null) return;

            // Summarize schedule as hour ranges per assignment type
            var schedule = new List<string>();
            string currentAssignment = null;
            int rangeStart = 0;

            for (int hour = 0; hour <= 24; hour++)
            {
                var assignment = hour < 24 ? timetable.times[hour]?.label ?? "anything" : null;

                if (assignment != currentAssignment)
                {
                    if (currentAssignment != null)
                    {
                        schedule.Add($"{rangeStart}-{hour}: {currentAssignment}");
                    }
                    currentAssignment = assignment;
                    rangeStart = hour;
                }
            }

            if (schedule.Count > 0)
                p.SetList("schedule", schedule);
        }
    }
}
