using System;
using HarmonyLib;
using RimWorld;
using SavecraftRimWorld.Collectors;
using SavecraftRimWorld.Connection;
using Verse;

namespace SavecraftRimWorld
{
    [StaticConstructorOnStartup]
    public static class SavecraftHarmonyInit
    {
        static SavecraftHarmonyInit()
        {
            var harmony = new Harmony("savecraft.rimworld");
            harmony.PatchAll();
            Log.Message("[Savecraft] Mod loaded, Harmony patches applied.");
        }
    }

    public class SavecraftMod : Mod
    {
        public static SavecraftSettings Settings { get; private set; }
        public static SavecraftConnection Connection { get; private set; }

        public SavecraftMod(ModContentPack content) : base(content)
        {
            Settings = GetSettings<SavecraftSettings>();
            Connection = new SavecraftConnection(Settings);
        }

        public override string SettingsCategory() => "Savecraft";

        public override void DoSettingsWindowContents(UnityEngine.Rect inRect)
        {
            var listing = new Listing_Standard();
            listing.Begin(inRect);

            listing.Label($"Server: {Settings.ServerUrl}");

            var statusText = Connection.Status switch
            {
                ConnectionStatus.Disconnected => "Disconnected",
                ConnectionStatus.Connecting => "Connecting...",
                ConnectionStatus.Connected => "Connected (not linked)",
                ConnectionStatus.Linked => "Linked",
                _ => "Unknown"
            };
            listing.Label($"Status: {statusText}");

            if (!string.IsNullOrEmpty(Settings.LinkCode) && !Settings.IsLinked)
            {
                listing.Gap(12f);
                listing.Label($"Link Code: {Settings.LinkCode}");
                listing.Label("Enter this code at savecraft.gg to link your colony.");
            }

            listing.End();
        }
    }

    public class SavecraftSettings : ModSettings
    {
        public string ServerUrl = "wss://savecraft.gg";
        public string SourceUuid = "";
        public string SourceToken = "";
        public string LinkCode = "";
        public bool IsLinked;

        public override void ExposeData()
        {
            Scribe_Values.Look(ref ServerUrl, "serverUrl", "wss://savecraft.gg");
            Scribe_Values.Look(ref SourceUuid, "sourceUuid", "");
            Scribe_Values.Look(ref SourceToken, "sourceToken", "");
            Scribe_Values.Look(ref LinkCode, "linkCode", "");
            Scribe_Values.Look(ref IsLinked, "isLinked", false);
            base.ExposeData();
        }

        public bool HasCredentials => !string.IsNullOrEmpty(SourceUuid) && !string.IsNullOrEmpty(SourceToken);
    }

    public class SavecraftGameComponent : GameComponent
    {
        public SavecraftGameComponent(Game game) { }

        public override void FinalizeInit()
        {
            Log.Message("[Savecraft] Game started, initializing connection.");

            var runner = new CollectorRunner();
            runner.Register(new ColonyOverviewCollector());
            SavecraftMod.Connection.SetCollectorRunner(runner);

            SavecraftMod.Connection.Start();
        }

        public override void GameComponentUpdate()
        {
            // Drain main-thread work queue (WebSocket callbacks that need Unity main thread)
            var queue = SavecraftMod.Connection.MainThreadQueue;
            while (queue.TryDequeue(out var action))
            {
                try
                {
                    action();
                }
                catch (Exception ex)
                {
                    Log.Error($"[Savecraft] Main thread callback error: {ex}");
                }
            }
        }

        public override void GameComponentOnGUI()
        {
            // Future: overlay UI for link code display
        }
    }
}
