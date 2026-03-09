using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Helpers for building Google.Protobuf.WellKnownTypes.Struct from C# types.
    /// Proto Struct is the wire format for section data (arbitrary JSON).
    /// </summary>
    public static class StructHelper
    {
        public static Struct NewStruct()
        {
            return new Struct();
        }

        public static void Set(this Struct s, string key, string value)
        {
            s.Fields[key] = Value.ForString(value ?? "");
        }

        public static void Set(this Struct s, string key, double value)
        {
            s.Fields[key] = Value.ForNumber(value);
        }

        public static void Set(this Struct s, string key, bool value)
        {
            s.Fields[key] = Value.ForBool(value);
        }

        public static void Set(this Struct s, string key, Struct value)
        {
            s.Fields[key] = Value.ForStruct(value);
        }

        public static void SetList(this Struct s, string key, IEnumerable<string> items)
        {
            var list = new ListValue();
            foreach (var item in items)
            {
                list.Values.Add(Value.ForString(item));
            }
            s.Fields[key] = new Value { ListValue = list };
        }

        public static void SetList(this Struct s, string key, IEnumerable<Struct> items)
        {
            var list = new ListValue();
            foreach (var item in items)
            {
                list.Values.Add(Value.ForStruct(item));
            }
            s.Fields[key] = new Value { ListValue = list };
        }

        /// <summary>
        /// Classify a hediff into a type string for consistent section data.
        /// </summary>
        public static string ClassifyHediff(Hediff hediff)
        {
            if (hediff is Hediff_Injury) return "injury";
            if (hediff is Hediff_MissingPart) return "missing_part";
            if (hediff is Hediff_AddedPart) return "implant";
            if (hediff.def.lethalSeverity > 0) return "disease";
            return "condition";
        }

        /// <summary>
        /// Increment a count in a dictionary, initializing to 1 if absent.
        /// </summary>
        public static void Increment(this Dictionary<string, int> dict, string key)
        {
            if (dict.TryGetValue(key, out int count))
                dict[key] = count + 1;
            else
                dict[key] = 1;
        }

        /// <summary>
        /// Add a float value to a dictionary key, initializing to 0 if absent.
        /// </summary>
        public static void Add(this Dictionary<string, float> dict, string key, float value)
        {
            if (dict.TryGetValue(key, out float existing))
                dict[key] = existing + value;
            else
                dict[key] = value;
        }
    }
}
