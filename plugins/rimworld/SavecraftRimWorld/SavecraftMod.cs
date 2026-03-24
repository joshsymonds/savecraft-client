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

            listing.Gap(12f);

            if (Settings.IsLinked)
            {
                listing.Label("Your colony is linked to your Savecraft account.");
                listing.Gap(6f);
                if (listing.ButtonText("Re-pair with a different account"))
                {
                    Connection.RequestNewLinkCode();
                }
            }
            else if (!string.IsNullOrEmpty(Settings.LinkCode))
            {
                listing.Label($"Link Code: {Settings.LinkCode}");
                listing.Label("Enter this code at savecraft.gg to link your colony.");
                listing.Gap(6f);
                if (listing.ButtonText("Get new link code"))
                {
                    Connection.RequestNewLinkCode();
                }
            }
            else if (Settings.HasCredentials)
            {
                listing.Label("Waiting for link code from server...");
            }
            else
            {
                listing.Label("Not registered. Start a game to register automatically.");
            }

            listing.End();
        }
    }

    public class SavecraftSettings : ModSettings
    {
        public string ServerUrl = "wss://api.savecraft.gg";
        public string SourceUuid = "";
        public string SourceToken = "";
        public string LinkCode = "";
        public bool IsLinked;

        public override void ExposeData()
        {
            Scribe_Values.Look(ref ServerUrl, "serverUrl", "wss://api.savecraft.gg");
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
            // Core sections
            runner.Register(new ColonyOverviewCollector());
            runner.Register(new ColonistRosterCollector());
            runner.Register(new ResourcesCollector());
            runner.Register(new ResearchCollector());
            runner.Register(new SkillsAndWorkCollector());
            runner.Register(new MoodReportCollector());
            runner.Register(new HealthReportCollector());
            // Infrastructure sections
            runner.Register(new PowerCollector());
            runner.Register(new FarmingCollector());
            runner.Register(new DefensesCollector());
            runner.Register(new RoomsCollector());
            // World sections
            runner.Register(new FactionsCollector());
            runner.Register(new ThreatsCollector());
            runner.Register(new AnimalsCollector());
            // Dynamic per-colonist sections
            runner.Register(new ColonistDetailCollector());
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
    }
}
