using HarmonyLib;
using RimWorld;
using Verse;

namespace SavecraftRimWorld.Patches
{
    /// <summary>
    /// Harmony postfix on GameDataSaveLoader.SaveGame.
    /// Fires after every save (autosave and manual), triggering data collection and push.
    /// </summary>
    [HarmonyPatch(typeof(GameDataSaveLoader), nameof(GameDataSaveLoader.SaveGame))]
    public static class SaveGamePatch
    {
        static void Postfix(string fileName)
        {
            Log.Message($"[Savecraft] Save detected: {fileName}, collecting colony data...");
            SavecraftMod.Connection?.OnSave();
        }
    }
}
