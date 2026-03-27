using System;
using HarmonyLib;
using RimWorld;
using SavecraftRimWorld.Collectors;
using SavecraftRimWorld.Connection;
using SavecraftRimWorld.UI;
using UnityEngine;
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
        public const string Version = "0.1.0";

        // Savecraft brand colors
        static readonly Color PanelBg = new Color(0.039f, 0.055f, 0.180f, 1f);       // #0a0e2e
        static readonly Color BorderColor = new Color(0.290f, 0.353f, 0.678f, 1f);    // #4a5aad
        static readonly Color Gold = new Color(0.784f, 0.659f, 0.306f, 1f);           // #c8a84e
        static readonly Color GoldLight = new Color(0.910f, 0.784f, 0.431f, 1f);      // #e8c86e
        static readonly Color StatusGreen = new Color(0.353f, 0.745f, 0.541f, 1f);    // #5abe8a
        static readonly Color StatusYellow = new Color(0.910f, 0.769f, 0.306f, 1f);   // #e8c44e
        static readonly Color StatusRed = new Color(0.910f, 0.353f, 0.353f, 1f);      // #e85a5a
        static readonly Color TextMuted = new Color(0.627f, 0.659f, 0.800f, 1f);      // #a0a8cc
        static readonly Color DividerColor = new Color(0.290f, 0.353f, 0.678f, 0.4f); // #4a5aad @ 40%

        static Texture2D brandIcon;
        static bool brandIconLoaded;

        public static SavecraftSettings Settings { get; private set; }
        public static SavecraftConnection Connection { get; private set; }

        public SavecraftMod(ModContentPack content) : base(content)
        {
            Settings = GetSettings<SavecraftSettings>();
            Connection = new SavecraftConnection(Settings);
        }

        public override string SettingsCategory() => "Savecraft";

        public override void DoSettingsWindowContents(Rect inRect)
        {
            if (!brandIconLoaded)
            {
                brandIconLoaded = true;
                brandIcon = ContentFinder<Texture2D>.Get("Savecraft/SyncIcon", false);
            }

            var listing = new Listing_Standard();
            listing.Begin(inRect);

            DrawHeader(listing);
            listing.Gap(8f);
            DrawStatusPanel(listing);
            listing.Gap(12f);
            DrawDivider(listing);
            listing.Gap(12f);
            DrawLinkSection(listing);
            listing.Gap(12f);
            DrawActions(listing);

            listing.End();
        }

        void DrawHeader(Listing_Standard listing)
        {
            var headerRect = listing.GetRect(32f);
            var prevColor = GUI.color;
            var prevFont = Text.Font;
            var prevAnchor = Text.Anchor;

            // Icon
            if (brandIcon != null)
            {
                var iconRect = new Rect(headerRect.x, headerRect.y + 4f, 24f, 24f);
                GUI.DrawTexture(iconRect, brandIcon);
            }

            // Title in gold
            Text.Font = GameFont.Medium;
            Text.Anchor = TextAnchor.MiddleLeft;
            GUI.color = Gold;
            var titleRect = new Rect(headerRect.x + 32f, headerRect.y, 200f, 32f);
            Widgets.Label(titleRect, "Savecraft");

            // Version in muted text, right-aligned
            Text.Font = GameFont.Tiny;
            Text.Anchor = TextAnchor.MiddleRight;
            GUI.color = TextMuted;
            var versionRect = new Rect(headerRect.xMax - 120f, headerRect.y, 120f, 32f);
            Widgets.Label(versionRect, $"v{Version}");

            GUI.color = prevColor;
            Text.Font = prevFont;
            Text.Anchor = prevAnchor;
        }

        void DrawStatusPanel(Listing_Standard listing)
        {
            // Panel background with border
            const float panelHeight = 90f;
            const float padding = 10f;
            const float lineHeight = 22f;
            var panelRect = listing.GetRect(panelHeight);

            // Background
            Widgets.DrawBoxSolid(panelRect, PanelBg);

            // Border (draw 4 edges)
            Widgets.DrawBoxSolid(new Rect(panelRect.x, panelRect.y, panelRect.width, 1f), BorderColor);
            Widgets.DrawBoxSolid(new Rect(panelRect.x, panelRect.yMax - 1f, panelRect.width, 1f), BorderColor);
            Widgets.DrawBoxSolid(new Rect(panelRect.x, panelRect.y, 1f, panelRect.height), BorderColor);
            Widgets.DrawBoxSolid(new Rect(panelRect.xMax - 1f, panelRect.y, 1f, panelRect.height), BorderColor);

            var prevColor = GUI.color;
            var prevFont = Text.Font;
            var prevAnchor = Text.Anchor;
            Text.Anchor = TextAnchor.MiddleLeft;

            float y = panelRect.y + padding;
            float x = panelRect.x + padding;
            float w = panelRect.width - padding * 2f;

            // Connection status with colored dot
            var (statusText, dotColor) = Connection.Status switch
            {
                ConnectionStatus.Linked => ("Linked", StatusGreen),
                ConnectionStatus.Connected => ("Connected (not linked)", StatusYellow),
                ConnectionStatus.Connecting => ("Connecting...", StatusYellow),
                _ => ("Disconnected", StatusRed)
            };

            // Dot
            var dotRect = new Rect(x, y + 6f, 10f, 10f);
            Widgets.DrawBoxSolid(dotRect, dotColor);

            // Status text
            Text.Font = GameFont.Small;
            GUI.color = Color.white;
            var statusRect = new Rect(x + 16f, y, w - 16f, lineHeight);
            Widgets.Label(statusRect, $"Connection: {statusText}");
            y += lineHeight;

            // Last sync
            Text.Font = GameFont.Tiny;
            GUI.color = TextMuted;
            var syncText = Connection.LastSyncTime == default
                ? "Last sync: never"
                : $"Last sync: {FormatTimeAgo(Connection.LastSyncTime)}";
            var syncRect = new Rect(x + 16f, y, w - 16f, lineHeight);
            Widgets.Label(syncRect, syncText);
            y += lineHeight;

            // Section count
            var sectionText = Connection.LastSectionCount > 0
                ? $"Sections: {Connection.LastSectionCount} sections pushed"
                : "Sections: —";
            var sectionRect = new Rect(x + 16f, y, w - 16f, lineHeight);
            Widgets.Label(sectionRect, sectionText);

            GUI.color = prevColor;
            Text.Font = prevFont;
            Text.Anchor = prevAnchor;
        }

        void DrawDivider(Listing_Standard listing)
        {
            var divRect = listing.GetRect(1f);
            Widgets.DrawBoxSolid(divRect, DividerColor);
        }

        void DrawLinkSection(Listing_Standard listing)
        {
            var prevColor = GUI.color;
            var prevFont = Text.Font;
            var prevAnchor = Text.Anchor;

            if (Settings.IsLinked)
            {
                GUI.color = StatusGreen;
                Text.Font = GameFont.Small;
                listing.Label("Your colony is linked to your Savecraft account.");
                GUI.color = prevColor;
            }
            else if (!string.IsNullOrEmpty(Settings.LinkCode))
            {
                // Link code in gold, prominent
                Text.Font = GameFont.Small;
                GUI.color = GoldLight;
                listing.Label($"Link Code: {Settings.LinkCode}");
                GUI.color = TextMuted;
                Text.Font = GameFont.Tiny;
                listing.Label("Enter this code at savecraft.gg to link your colony.");
                GUI.color = prevColor;
                Text.Font = prevFont;

                listing.Gap(6f);
                if (listing.ButtonText("Get new link code"))
                {
                    Connection.RequestNewLinkCode();
                }
            }
            else if (Settings.HasCredentials)
            {
                GUI.color = TextMuted;
                listing.Label("Waiting for link code from server...");
                GUI.color = prevColor;
            }
            else
            {
                GUI.color = TextMuted;
                listing.Label("Not registered. Start a game to register automatically.");
                GUI.color = prevColor;
            }

            Text.Font = prevFont;
            Text.Anchor = prevAnchor;
        }

        void DrawActions(Listing_Standard listing)
        {
            var buttonRect = listing.GetRect(30f);
            float buttonWidth = (buttonRect.width - 8f) / 2f;

            // Test Push button
            var testRect = new Rect(buttonRect.x, buttonRect.y, buttonWidth, 30f);
            if (Widgets.ButtonText(testRect, "Test Push"))
            {
                Connection.ResetSyncState();
                Connection.OnSave();
            }

            // Re-pair button (only when linked)
            if (Settings.IsLinked)
            {
                var repairRect = new Rect(buttonRect.x + buttonWidth + 8f, buttonRect.y, buttonWidth, 30f);
                if (Widgets.ButtonText(repairRect, "Re-pair account"))
                {
                    Connection.RequestNewLinkCode();
                }
            }
        }

        static string FormatTimeAgo(DateTime utcTime)
        {
            var elapsed = DateTime.UtcNow - utcTime;
            if (elapsed.TotalSeconds < 5) return "just now";
            if (elapsed.TotalSeconds < 60) return $"{(int)elapsed.TotalSeconds}s ago";
            if (elapsed.TotalMinutes < 60) return $"{(int)elapsed.TotalMinutes}m ago";
            if (elapsed.TotalHours < 24) return $"{(int)elapsed.TotalHours}h ago";
            return $"{(int)elapsed.TotalDays}d ago";
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
        readonly SyncOverlay syncOverlay = new SyncOverlay();

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

        public override void GameComponentOnGUI()
        {
            syncOverlay.OnGUI();
        }
    }
}
