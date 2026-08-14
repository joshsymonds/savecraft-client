using System;
using System.Collections.Generic;
using System.Text;
using Google.Protobuf;
using Google.Protobuf.WellKnownTypes;

namespace SavecraftRimWorld.Collectors
{
    public static class ReportPartitioner
    {
        public const int RESULT_UNIT_BYTES = 10_240;

        public sealed class ReportSection
        {
            public string Name { get; set; }
            public Struct Data { get; set; }
        }

        public static List<ReportSection> Partition(string reportName, Struct report)
        {
            if (JsonSize(report) <= RESULT_UNIT_BYTES)
                return new List<ReportSection> { new ReportSection { Name = reportName, Data = report } };

            if (!report.Fields.TryGetValue("colonists", out var colonistsValue) ||
                colonistsValue.KindCase != Value.KindOneofCase.ListValue)
                throw new InvalidOperationException($"Report '{reportName}' has no colonists list to partition.");

            var sections = new List<ReportSection>();
            var slugCounts = new Dictionary<string, int>(StringComparer.Ordinal);
            foreach (var value in colonistsValue.ListValue.Values)
            {
                if (value.KindCase != Value.KindOneofCase.StructValue)
                    throw new InvalidOperationException($"Report '{reportName}' contains a non-struct colonist.");

                var colonist = value.StructValue;
                var colonistName = ColonistName(colonist);
                var slug = Slug(colonistName);
                slugCounts.TryGetValue(slug, out var duplicateCount);
                slugCounts[slug] = ++duplicateCount;

                var unit = report.Clone();
                unit.Fields["colonists"] = new Value
                {
                    ListValue = new ListValue { Values = { Value.ForStruct(colonist.Clone()) } }
                };
                unit.Fields["count"] = Value.ForNumber(1);
                sections.Add(new ReportSection
                {
                    Name = $"{reportName}:{slug}{(duplicateCount == 1 ? "" : $"-{duplicateCount}")}",
                    Data = unit
                });
            }

            return sections;
        }

        static string ColonistName(Struct colonist)
        {
            return colonist.Fields.TryGetValue("name", out var name) &&
                   name.KindCase == Value.KindOneofCase.StringValue &&
                   !string.IsNullOrWhiteSpace(name.StringValue)
                ? name.StringValue.Trim()
                : "Unknown";
        }

        static string Slug(string value)
        {
            var slug = new StringBuilder();
            var separatorPending = false;
            foreach (var character in value.ToLowerInvariant())
            {
                if ((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9'))
                {
                    if (separatorPending && slug.Length > 0)
                        slug.Append('-');
                    slug.Append(character);
                    separatorPending = false;
                }
                else if (character == ' ' || character == '_' || character == '-')
                {
                    separatorPending = slug.Length > 0;
                }
            }
            return slug.Length == 0 ? "unknown" : slug.ToString();
        }

        static int JsonSize(Struct value) => Encoding.UTF8.GetByteCount(JsonFormatter.Default.Format(value));
    }
}
