using System.Collections.Generic;
using System.Linq;
using Google.Protobuf.WellKnownTypes;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects key room quality data.
    /// Answers: "Are my colonists happy with their rooms?", "Which rooms need improvement?",
    /// "What's the hospital impressiveness?", "Is the prison adequate?"
    /// </summary>
    public class RoomsCollector : ICollector
    {
        public string SectionName => "rooms";

        public string Description =>
            "Key room quality. " +
            "Per room: role (bedroom/dining/rec/hospital/prison), temperature, " +
            "impressiveness, beauty, cleanliness, space. " +
            "Only enclosed rooms with a role — not every hallway segment.";

        public Struct Collect()
        {
            var s = StructHelper.NewStruct();
            var map = Find.CurrentMap;
            var rooms = new List<Struct>();

            // Collect unique rooms via regions (regionGrid.allRooms is not always accessible)
            var seenRooms = new HashSet<int>();
            var allRooms = new List<Room>();
            foreach (var region in map.regionGrid.AllRegions)
            {
                var room = region.Room;
                if (room != null && seenRooms.Add(room.ID))
                    allRooms.Add(room);
            }

            foreach (var room in allRooms)
            {
                // Skip outdoors, very small rooms, and rooms without a role
                if (room.TouchesMapEdge) continue;
                if (room.CellCount < 4) continue;

                var role = room.Role;
                if (role == null) continue;

                var r = StructHelper.NewStruct();
                r.Set("role", role.label);
                r.Set("size", room.CellCount);
                r.Set("temperature", System.Math.Round(room.Temperature, 1));
                r.Set("impressiveness", System.Math.Round(room.GetStat(RoomStatDefOf.Impressiveness), 1));
                r.Set("beauty", System.Math.Round(room.GetStat(RoomStatDefOf.Beauty), 1));
                r.Set("cleanliness", System.Math.Round(room.GetStat(RoomStatDefOf.Cleanliness), 2));
                r.Set("space", System.Math.Round(room.GetStat(RoomStatDefOf.Space), 1));

                rooms.Add(r);
            }

            s.SetList("rooms", rooms);
            s.Set("count", rooms.Count);
            return s;
        }
    }
}
