package pluginmgr

import (
	"reflect"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/manifest"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

type clientRegistryManifest struct {
	Plugins map[string]PluginInfo `json:"plugins"`
}

func TestClientOnlyRegistryCompatibility(t *testing.T) {
	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	// This is the post-split public aggregate: client descriptors plus only the
	// fields the daemon consumes. Private reference and adapter data, and
	// API-only games, are deliberately absent.
	const registryJSON = `{
  "plugins": {
    "sdv": {
      "game_id": "sdv",
      "name": "Stardew Valley",
      "version": "1.6.14",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "url": "https://api.savecraft.gg/plugins/sdv/parser.wasm",
      "default_paths": {
        "windows": "%APPDATA%/StardewValley/Saves",
        "linux": "~/.config/StardewValley/Saves",
        "darwin": "~/.config/StardewValley/Saves"
      },
      "file_extensions": [".xml"],
      "file_patterns": ["SaveGameInfo", "*_old"],
      "exclude_dirs": ["ErrorLogs", "Screenshots"],
      "sources": ["wasm"],
      "icon": "icon.png",
      "icon_url": "https://api.savecraft.gg/plugins/sdv/icon.png",
      "description": "Parses Stardew Valley save files into structured game state",
      "channel": "stable",
      "coverage": "partial",
      "limitations": ["Only Stardew Valley 1.6+ saves are supported"],
      "author": {"name": "Josh Symonds", "github": "joshsymonds"},
      "homepage": "https://savecraft.gg/plugins/sdv",
      "workshop_url": "https://steamcommunity.com/app/413150/workshop/"
    },
    "rimworld": {
      "game_id": "rimworld",
      "name": "RimWorld",
      "version": "1.0.0",
      "sources": ["mod"],
      "icon": "icon.png",
      "icon_url": "https://api.savecraft.gg/plugins/rimworld/icon.png",
      "description": "An in-game mod publishes colony state",
      "channel": "alpha",
      "coverage": "full",
      "limitations": ["Vanilla data only"],
      "author": {"name": "Josh Symonds", "github": "joshsymonds"},
      "homepage": "https://savecraft.gg/plugins/rimworld",
      "workshop_url": "https://steamcommunity.com/sharedfiles/filedetails/?id=3693580596",
      "default_paths": {"windows": "", "linux": "", "darwin": ""},
      "file_extensions": null
    }
  }
}`
	raw := []byte(registryJSON)
	sig := signing.Sign(priv, raw)

	got, err := manifest.VerifyAndParse[clientRegistryManifest](pub, raw, sig)
	if err != nil {
		t.Fatalf("VerifyAndParse client registry: %v", err)
	}
	if len(got.Plugins) != 2 {
		t.Fatalf("plugins = %v, want exactly the two client-visible games", got.Plugins)
	}

	wantSDV := PluginInfo{
		GameID:  "sdv",
		Name:    "Stardew Valley",
		Version: "1.6.14",
		SHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		URL:     "https://api.savecraft.gg/plugins/sdv/parser.wasm",
		DefaultPaths: map[string]string{
			"windows": "%APPDATA%/StardewValley/Saves",
			"linux":   "~/.config/StardewValley/Saves",
			"darwin":  "~/.config/StardewValley/Saves",
		},
		FileExtensions: []string{".xml"},
		FilePatterns:   []string{"SaveGameInfo", "*_old"},
		ExcludeDirs:    []string{"ErrorLogs", "Screenshots"},
	}
	if sdv := got.Plugins["sdv"]; !reflect.DeepEqual(sdv, wantSDV) {
		t.Errorf("sdv = %#v, want %#v", sdv, wantSDV)
	}

	rimworld, ok := got.Plugins["rimworld"]
	if !ok {
		t.Fatal("mod-only rimworld entry was not decoded")
	}
	if rimworld.SHA256 != "" || rimworld.URL != "" {
		t.Errorf("mod-only payload fields = sha256 %q, url %q; want both empty", rimworld.SHA256, rimworld.URL)
	}

	const origin = "https://api.savecraft.gg"
	if err := manifest.RequirePinnedHTTPS(wantSDV.URL, origin); err != nil {
		t.Errorf("RequirePinnedHTTPS fixture URL: %v", err)
	}
	if err := manifest.RequirePinnedHTTPS("https://cdn.savecraft.gg/plugins/sdv/parser.wasm", origin); err == nil {
		t.Error("RequirePinnedHTTPS accepted an off-origin plugin URL")
	}

	const workshopField = `      "workshop_url": "https://steamcommunity.com/app/413150/workshop/"`
	forwardJSON := strings.Replace(
		registryJSON,
		workshopField,
		workshopField+`,
      "future_client_field": {"format": "v2"}`,
		1,
	)
	if forwardJSON == registryJSON {
		t.Fatal("failed to add the forward-compatible fixture field")
	}
	forwardRaw := []byte(forwardJSON)
	forwardSig := signing.Sign(priv, forwardRaw)
	forward, err := manifest.VerifyAndParse[clientRegistryManifest](pub, forwardRaw, forwardSig)
	if err != nil {
		t.Fatalf("VerifyAndParse registry with unknown field: %v", err)
	}
	if sdv := forward.Plugins["sdv"]; !reflect.DeepEqual(sdv, wantSDV) {
		t.Errorf("sdv after unknown field = %#v, want %#v", sdv, wantSDV)
	}
}
