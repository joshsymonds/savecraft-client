using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using Google.Protobuf;
using Google.Protobuf.WellKnownTypes;
using SavecraftRimWorld.Collectors;
using Xunit;

namespace SavecraftRimWorld.Tests
{
    public class ReportPartitionerTests
    {
        public static IEnumerable<object[]> Reports()
        {
            yield return new object[] { "health_report" };
            yield return new object[] { "skills_and_work" };
            yield return new object[] { "mood_report" };
            yield return new object[] { "relationships" };
        }

        [Theory]
        [MemberData(nameof(Reports))]
        public void LargeReportPartitionsPerColonistWithinCeilingAndReassembles(string reportName)
        {
            var source = Report("Alex", "Alex", "Bree", "Casey");
            source.Fields["metadata"] = Value.ForString("preserved");

            var sections = ReportPartitioner.Partition(reportName, source);

            Assert.Equal(4, sections.Count);
            Assert.All(sections, section => Assert.InRange(JsonSize(section.Data), 1, ReportPartitioner.RESULT_UNIT_BYTES));
            var reassembled = source.Clone();
            reassembled.Fields["colonists"] = new Value
            {
                ListValue = new ListValue
                {
                    Values = { sections.Select(section => Value.ForStruct(Colonists(section.Data).Single().Clone())) }
                }
            };
            reassembled.Fields["count"] = Value.ForNumber(sections.Count);
            Assert.Equal(source, reassembled);
            Assert.All(sections, section => Assert.Equal("preserved", section.Data.Fields["metadata"].StringValue));
        }

        [Theory]
        [MemberData(nameof(Reports))]
        public void SmallReportKeepsOriginalSingleSection(string reportName)
        {
            var source = Report("Alex");

            var sections = ReportPartitioner.Partition(reportName, source);

            var section = Assert.Single(sections);
            Assert.Equal(reportName, section.Name);
            Assert.Same(source, section.Data);
        }

        [Fact]
        public void ReportOverJsonCeilingButUnderProtobufCeilingPartitions()
        {
            var source = Report(new[] { "Alex", "Bree" }, 900, '\u0001');

            Assert.InRange(source.CalculateSize(), 1, ReportPartitioner.RESULT_UNIT_BYTES);
            Assert.True(JsonSize(source) > ReportPartitioner.RESULT_UNIT_BYTES);

            var sections = ReportPartitioner.Partition("health_report", source);

            Assert.Equal(new[] { "health_report:alex", "health_report:bree" }, sections.Select(section => section.Name));
            Assert.All(sections, section => Assert.InRange(JsonSize(section.Data), 1, ReportPartitioner.RESULT_UNIT_BYTES));
        }

        [Fact]
        public void NamesAreDeterministicPrefixedSluggedAndUnique()
        {
            var source = Report("  Alex Smith  ", "Alex Smith", "Ålex!? Smith");

            var first = ReportPartitioner.Partition("health_report", source).Select(section => section.Name).ToArray();
            var second = ReportPartitioner.Partition("health_report", source).Select(section => section.Name).ToArray();

            Assert.Equal(new[] { "health_report:alex-smith", "health_report:alex-smith-2", "health_report:lex-smith" }, first);
            Assert.Equal(first, second);
        }

        [Fact]
        public void NaturalNumericSuffixCollisionStillProducesUniqueNames()
        {
            var source = Report("Alex", "Alex-2", "Alex");

            var names = ReportPartitioner.Partition("health_report", source)
                .Select(section => section.Name)
                .ToArray();

            Assert.Equal(new[] { "health_report:alex", "health_report:alex-2", "health_report:alex-3" }, names);
        }

        [Fact]
        public void EmitsAColonistThatCannotFitWithinTheCeiling()
        {
            var source = Report("Alex", payloadBytes: ReportPartitioner.RESULT_UNIT_BYTES * 2);

            var section = Assert.Single(ReportPartitioner.Partition("health_report", source));

            Assert.Equal("health_report:alex", section.Name);
            Assert.True(JsonSize(section.Data) > ReportPartitioner.RESULT_UNIT_BYTES);
            var warning = Assert.Single(section.Warnings);
            Assert.Equal(section.Name, warning.SectionName);
            Assert.Equal(JsonSize(section.Data), warning.ByteSize);
        }

        [Fact]
        public void StopsPartitioningAtTheSectionBudgetAndReturnsAWarning()
        {
            var source = Report("Alex", "Bree", "Casey");

            var result = ReportPartitioner.Partition("health_report", source, 2);

            Assert.Equal(2, result.Sections.Count);
            var warning = Assert.Single(result.Warnings);
            Assert.Equal("health_report", warning.SectionName);
            Assert.Contains("section cap", warning.Message, StringComparison.OrdinalIgnoreCase);
        }

        static Struct Report(params string[] names) => Report(names, 4_000);

        static Struct Report(string name, int payloadBytes) => Report(new[] { name }, payloadBytes);

        static Struct Report(string[] names, int payloadBytes, char payloadCharacter = 'a')
        {
            var report = new Struct();
            var list = new ListValue();
            for (var index = 0; index < names.Length; index++)
            {
                var colonist = new Struct();
                colonist.Fields["name"] = Value.ForString(names[index]);
                colonist.Fields["marker"] = Value.ForString($"{names[index]}:{index}");
                colonist.Fields["payload"] = Value.ForString(new string(payloadCharacter, payloadBytes));
                list.Values.Add(Value.ForStruct(colonist));
            }
            report.Fields["colonists"] = new Value { ListValue = list };
            report.Fields["count"] = Value.ForNumber(names.Length);
            return report;
        }

        static IEnumerable<Struct> Colonists(Struct report) =>
            report.Fields["colonists"].ListValue.Values.Select(value => value.StructValue);

        static int JsonSize(Struct value) => Encoding.UTF8.GetByteCount(JsonFormatter.Default.Format(value));
    }
}
