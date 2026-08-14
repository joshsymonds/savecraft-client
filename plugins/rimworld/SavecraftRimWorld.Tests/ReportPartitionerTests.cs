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

            var sections = ReportPartitioner.Partition(reportName, source);

            Assert.Equal(4, sections.Count);
            Assert.All(sections, section => Assert.InRange(JsonSize(section.Data), 1, ReportPartitioner.RESULT_UNIT_BYTES));
            Assert.Equal(
                new[] { "Alex:0", "Alex:1", "Bree:2", "Casey:3" },
                sections.Select(section => Colonists(section.Data).Single().Fields["marker"].StringValue));
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
        public void EmitsAColonistThatCannotFitWithinTheCeiling()
        {
            var source = Report("Alex", payloadBytes: ReportPartitioner.RESULT_UNIT_BYTES * 2);

            var section = Assert.Single(ReportPartitioner.Partition("health_report", source));

            Assert.Equal("health_report:alex", section.Name);
            Assert.True(JsonSize(section.Data) > ReportPartitioner.RESULT_UNIT_BYTES);
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
