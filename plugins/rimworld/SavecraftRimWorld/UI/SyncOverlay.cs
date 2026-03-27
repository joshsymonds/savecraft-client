using SavecraftRimWorld.Connection;
using UnityEngine;
using Verse;

namespace SavecraftRimWorld.UI
{
    /// <summary>
    /// Draws a small Savecraft icon in the bottom-right corner during sync events.
    /// Called from GameComponentOnGUI on every frame — only draws when sync is active.
    /// All animation timing uses realtimeSinceStartup (game-speed independent).
    /// </summary>
    public class SyncOverlay
    {
        const float IconSize = 32f;
        const float Margin = 8f;
        const float FadeInDuration = 0.3f;
        const float FadeOutDuration = 0.5f;
        const float SuccessHoldDuration = 2f;
        const float ErrorHoldDuration = 5f;
        const float PulseFrequency = 2f; // Hz
        const float PulseMin = 0.6f;
        const float PulseMax = 1f;

        static readonly Color ErrorTint = new Color(1f, 0.3f, 0.3f, 1f);

        Texture2D icon;
        bool textureLoaded;

        // Animation state
        SyncState lastObservedState = SyncState.Idle;
        SyncState displayState = SyncState.Idle;
        float stateEnteredAt;
        float holdCompleteAt;

        void EnsureTexture()
        {
            if (textureLoaded) return;
            textureLoaded = true;
            icon = ContentFinder<Texture2D>.Get("Savecraft/SyncIcon", false);
            if (icon == null)
                Log.Warning("[Savecraft] Could not load Savecraft/SyncIcon texture.");
        }

        public void OnGUI()
        {
            EnsureTexture();
            if (icon == null) return;

            var connection = SavecraftMod.Connection;
            if (connection == null) return;

            var currentState = connection.CurrentSyncState;
            UpdateDisplayState(currentState);

            if (displayState == SyncState.Idle) return;

            float alpha = CalculateAlpha();
            if (alpha <= 0f) return;

            var prevColor = GUI.color;
            var tint = displayState == SyncState.Error ? ErrorTint : Color.white;
            tint.a = alpha;
            GUI.color = tint;

            var rect = new Rect(
                UnityEngine.Screen.width - IconSize - Margin,
                UnityEngine.Screen.height - IconSize - Margin,
                IconSize,
                IconSize);
            GUI.DrawTexture(rect, icon);

            GUI.color = prevColor;
        }

        void UpdateDisplayState(SyncState currentState)
        {
            if (currentState != lastObservedState)
            {
                lastObservedState = currentState;

                switch (currentState)
                {
                    case SyncState.Syncing:
                        displayState = SyncState.Syncing;
                        stateEnteredAt = Time.realtimeSinceStartup;
                        break;

                    case SyncState.Success:
                        displayState = SyncState.Success;
                        stateEnteredAt = Time.realtimeSinceStartup;
                        holdCompleteAt = Time.realtimeSinceStartup + SuccessHoldDuration;
                        break;

                    case SyncState.Error:
                        displayState = SyncState.Error;
                        stateEnteredAt = Time.realtimeSinceStartup;
                        holdCompleteAt = Time.realtimeSinceStartup + ErrorHoldDuration;
                        break;

                    case SyncState.Idle:
                        // Connection reset to idle — if we were displaying, start fade out
                        if (displayState == SyncState.Syncing)
                        {
                            displayState = SyncState.Success;
                            stateEnteredAt = Time.realtimeSinceStartup;
                            holdCompleteAt = Time.realtimeSinceStartup + SuccessHoldDuration;
                        }
                        break;
                }
            }

            // Check if hold+fade has completed for Success/Error
            if (displayState == SyncState.Success || displayState == SyncState.Error)
            {
                float elapsed = Time.realtimeSinceStartup - holdCompleteAt;
                if (elapsed > FadeOutDuration)
                {
                    displayState = SyncState.Idle;
                }
            }
        }

        float CalculateAlpha()
        {
            float now = Time.realtimeSinceStartup;
            float elapsed = now - stateEnteredAt;

            switch (displayState)
            {
                case SyncState.Syncing:
                    // Fade in, then pulse
                    float fadeIn = Mathf.Clamp01(elapsed / FadeInDuration);
                    float pulse = Mathf.Lerp(PulseMin, PulseMax,
                        (Mathf.Sin(now * PulseFrequency * 2f * Mathf.PI) + 1f) / 2f);
                    return fadeIn * pulse;

                case SyncState.Success:
                case SyncState.Error:
                    float sinceHoldEnd = now - holdCompleteAt;
                    if (sinceHoldEnd < 0f) return 1f; // Still in hold period
                    return 1f - Mathf.Clamp01(sinceHoldEnd / FadeOutDuration);

                default:
                    return 0f;
            }
        }
    }
}
