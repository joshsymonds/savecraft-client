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
}
