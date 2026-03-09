using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;

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
    }
}
