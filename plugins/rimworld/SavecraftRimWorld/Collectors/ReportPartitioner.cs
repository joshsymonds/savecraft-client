using System;
using System.Collections.Generic;
using System.IO;
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
            public List<PartitionWarning> Warnings { get; } = new List<PartitionWarning>();
        }

        public sealed class PartitionWarning
        {
            public string SectionName { get; set; }
            public int ByteSize { get; set; }
            public string Message { get; set; }
        }

        public sealed class PartitionResult
        {
            public List<ReportSection> Sections { get; } = new List<ReportSection>();
            public List<PartitionWarning> Warnings { get; } = new List<PartitionWarning>();
        }

        public static List<ReportSection> Partition(string reportName, Struct report) =>
            Partition(reportName, report, int.MaxValue).Sections;

        public static PartitionResult Partition(string reportName, Struct report, int maxSections)
        {
            if (maxSections < 0)
                throw new ArgumentOutOfRangeException(nameof(maxSections));

            var result = new PartitionResult();
            if (maxSections == 0)
            {
                AddCapWarning(result, reportName, maxSections);
                return result;
            }

            if (FitsJsonSize(report))
            {
                result.Sections.Add(new ReportSection { Name = reportName, Data = report });
                return result;
            }

            if (!report.Fields.TryGetValue("colonists", out var colonistsValue) ||
                colonistsValue.KindCase != Value.KindOneofCase.ListValue)
                throw new InvalidOperationException($"Report '{reportName}' has no colonists list to partition.");

            var usedSlugs = new HashSet<string>(StringComparer.Ordinal);
            foreach (var value in colonistsValue.ListValue.Values)
            {
                if (result.Sections.Count >= maxSections)
                {
                    AddCapWarning(result, reportName, maxSections);
                    break;
                }

                if (value.KindCase != Value.KindOneofCase.StructValue)
                    throw new InvalidOperationException($"Report '{reportName}' contains a non-struct colonist.");

                var colonist = value.StructValue;
                var baseSlug = Slug(ColonistName(colonist));
                var slug = baseSlug;
                var suffix = 2;
                while (!usedSlugs.Add(slug))
                    slug = $"{baseSlug}-{suffix++}";

                var unit = new Struct();
                foreach (var field in report.Fields)
                {
                    if (field.Key != "colonists")
                        unit.Fields[field.Key] = field.Value.Clone();
                }
                unit.Fields["colonists"] = new Value
                {
                    ListValue = new ListValue { Values = { Value.ForStruct(colonist.Clone()) } }
                };
                unit.Fields["count"] = Value.ForNumber(1);
                var section = new ReportSection { Name = $"{reportName}:{slug}", Data = unit };
                if (!FitsJsonSize(unit))
                {
                    var byteSize = ExactJsonSize(unit);
                    section.Warnings.Add(new PartitionWarning
                    {
                        SectionName = section.Name,
                        ByteSize = byteSize,
                        Message = $"Section '{section.Name}' is {byteSize} bytes and exceeds the {RESULT_UNIT_BYTES}-byte limit."
                    });
                }
                result.Sections.Add(section);
                result.Warnings.AddRange(section.Warnings);
            }
            return result;
        }

        static void AddCapWarning(PartitionResult result, string reportName, int maxSections)
        {
            result.Warnings.Add(new PartitionWarning
            {
                SectionName = reportName,
                Message = $"Section cap ({maxSections}) reached while partitioning '{reportName}'."
            });
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

        static bool FitsJsonSize(Struct value)
        {
            try
            {
                JsonFormatter.Default.Format(value, new CappedUtf8CountingWriter(RESULT_UNIT_BYTES + 1));
                return true;
            }
            catch (SizeLimitExceededException)
            {
                return false;
            }
        }

        static int ExactJsonSize(Struct value) =>
            Encoding.UTF8.GetByteCount(JsonFormatter.Default.Format(value));

        sealed class SizeLimitExceededException : Exception { }

        sealed class CappedUtf8CountingWriter : TextWriter
        {
            readonly int limit;
            readonly Encoder encoder = Encoding.UTF8.GetEncoder();
            readonly byte[] buffer;
            readonly char[] characters = new char[256];
            int count;

            public CappedUtf8CountingWriter(int limit)
            {
                this.limit = limit;
                buffer = new byte[limit];
            }

            public override Encoding Encoding => Encoding.UTF8;

            public override void Write(char value)
            {
                characters[0] = value;
                Write(characters, 0, 1);
            }

            public override void Write(char[] value, int index, int count)
            {
                encoder.Convert(value, index, count, buffer, this.count, limit - this.count,
                    false, out _, out var bytesUsed, out var completed);
                this.count += bytesUsed;
                if (!completed || this.count >= limit)
                    throw new SizeLimitExceededException();
            }

            public override void Write(string value)
            {
                if (value == null)
                    return;
                for (var offset = 0; offset < value.Length; offset += characters.Length)
                {
                    var length = Math.Min(characters.Length, value.Length - offset);
                    value.CopyTo(offset, characters, 0, length);
                    Write(characters, 0, length);
                }
            }
        }
    }
}
