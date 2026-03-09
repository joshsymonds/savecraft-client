using System.Collections.Generic;
using Google.Protobuf.WellKnownTypes;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Collects a single section of game state for a PushSave message.
    /// Must be called on Unity's main thread (reads live game objects).
    /// </summary>
    public interface ICollector
    {
        /// <summary>Section name used as the key in PushSave (e.g. "colony_overview").</summary>
        string SectionName { get; }

        /// <summary>Tells the AI what questions this section answers.</summary>
        string Description { get; }

        /// <summary>Extract game state into a proto Struct. Called on main thread.</summary>
        Struct Collect();
    }

    /// <summary>
    /// A section of collected data with its name and description.
    /// </summary>
    public struct CollectedSection
    {
        public string Name;
        public string Description;
        public Struct Data;
    }

    /// <summary>
    /// Collects multiple sections of game state (e.g. one per colonist).
    /// Must be called on Unity's main thread.
    /// </summary>
    public interface IMultiCollector
    {
        /// <summary>Extract game state into multiple named sections.</summary>
        List<CollectedSection> CollectAll();
    }
}
