package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunSingle(t *testing.T) {
	tests := []struct {
		name       string
		pluginTOML string
		parserWASM string
		version    string
		want       func(string) string
	}{
		{
			name: "wasm game emits client fields only",
			pluginTOML: `game_id = "echo"
sources = ["wasm"]
icon = "echo.svg"
name = "Echo"
description = "Test plugin"
channel = "stable"
coverage = "full"
file_extensions = [".txt", ".echo"]
file_patterns = ["save-*.txt"]
exclude_dirs = ["cache", "backups"]
homepage = "https://example.com/echo"
workshop_url = "https://example.com/echo/workshop"
limitations = ["Test limitation"]

[attribution]
sources = ["publisher", "community"]

[author]
name = "Test Author"
github = "test-author"

[default_paths]
windows = "C:/Echo"
linux = "/var/lib/echo"
darwin = "~/Library/Echo"

[reference.modules.lookup]
name = "Lookup"
description = "Server-only reference module."

[adapter]
auth_provider = "example"
auth_flow = "oauth2"
scopes = ["saves:read"]
regions = ["us"]
`,
			parserWASM: "fake wasm bytes",
			version:    "1.2.3",
			want: func(hash string) string {
				return fmt.Sprintf(`{
  "game_id": "echo",
  "sources": [
    "wasm"
  ],
  "icon": "echo.svg",
  "name": "Echo",
  "description": "Test plugin",
  "channel": "stable",
  "coverage": "full",
  "file_extensions": [
    ".txt",
    ".echo"
  ],
  "file_patterns": [
    "save-*.txt"
  ],
  "exclude_dirs": [
    "cache",
    "backups"
  ],
  "homepage": "https://example.com/echo",
  "workshop_url": "https://example.com/echo/workshop",
  "limitations": [
    "Test limitation"
  ],
  "attribution": {
    "sources": [
      "publisher",
      "community"
    ]
  },
  "author": {
    "name": "Test Author",
    "github": "test-author"
  },
  "default_paths": {
    "windows": "C:/Echo",
    "linux": "/var/lib/echo",
    "darwin": "~/Library/Echo"
  },
  "version": "1.2.3",
  "sha256": "%s",
  "url": "plugins/echo/parser.wasm"
}
`, hash)
			},
		},
		{
			name: "mod only omits parser fields",
			pluginTOML: `game_id = "rimworld"
sources = ["mod"]
icon = "rimworld.svg"
name = "RimWorld"
description = "Mod-only plugin"
channel = "alpha"
coverage = "partial"
file_extensions = [".zip"]
file_patterns = ["Save*.rws"]
exclude_dirs = ["Config"]
homepage = "https://example.com/rimworld"
limitations = ["Requires the companion mod"]

[author]
name = "Test Author"
github = "test-author"

[default_paths]
windows = "%APPDATA%/RimWorld"
linux = "~/.config/rimworld"
darwin = "~/Library/Application Support/RimWorld"

[reference.modules.guide]
name = "Guide"
description = "Server-only reference module."
`,
			version: "2.0.0",
			want: func(_ string) string {
				return `{
  "game_id": "rimworld",
  "sources": [
    "mod"
  ],
  "icon": "rimworld.svg",
  "name": "RimWorld",
  "description": "Mod-only plugin",
  "channel": "alpha",
  "coverage": "partial",
  "file_extensions": [
    ".zip"
  ],
  "file_patterns": [
    "Save*.rws"
  ],
  "exclude_dirs": [
    "Config"
  ],
  "homepage": "https://example.com/rimworld",
  "limitations": [
    "Requires the companion mod"
  ],
  "author": {
    "name": "Test Author",
    "github": "test-author"
  },
  "default_paths": {
    "windows": "%APPDATA%/RimWorld",
    "linux": "~/.config/rimworld",
    "darwin": "~/Library/Application Support/RimWorld"
  },
  "version": "2.0.0"
}
`
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "plugin.toml"), tt.pluginTOML)
			if tt.parserWASM != "" {
				writeTestFile(t, filepath.Join(dir, "parser.wasm"), tt.parserWASM)
			}

			const committedManifest = "committed manifest\n"
			writeTestFile(t, filepath.Join(dir, "manifest.json"), committedManifest)

			if err := runSingle(dir, tt.version); err != nil {
				t.Fatalf("runSingle: %v", err)
			}

			got, err := os.ReadFile(filepath.Join(dir, "client-manifest.json"))
			if err != nil {
				t.Fatalf("read client-manifest.json: %v", err)
			}

			hash := sha256.Sum256([]byte(tt.parserWASM))
			if want := tt.want(fmt.Sprintf("%x", hash)); string(got) != want {
				t.Errorf("client-manifest.json mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(got, &fields); err != nil {
				t.Fatalf("decode client-manifest.json: %v", err)
			}
			for _, forbidden := range []string{"reference", "adapter"} {
				if _, ok := fields[forbidden]; ok {
					t.Errorf("client-manifest.json contains forbidden %q field", forbidden)
				}
			}

			manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatalf("read manifest.json: %v", err)
			}
			if string(manifest) != committedManifest {
				t.Errorf("manifest.json was modified: got %q, want %q", manifest, committedManifest)
			}
		})
	}
}

// TestRunSingle_DirectoryUnitFields confirms the directory-unit plugin
// fields (unit, members, min_daemon_version) and the binary-distribution
// license metadata (license, source_url) pass through plugin.toml into
// client-manifest.json unchanged, alongside the existing fields.
func TestRunSingle_DirectoryUnitFields(t *testing.T) {
	const pluginTOMLSrc = `game_id = "palworld"
sources = ["wasm"]
icon = "palworld.svg"
name = "Palworld"
description = "Parses Palworld save directories into structured game state"
channel = "alpha"
coverage = "partial"
file_extensions = []
exclude_dirs = ["Backup"]
homepage = "https://example.com/palworld"
limitations = ["Test limitation"]
unit = "directory"
members = ["Level.sav", "LevelMeta.sav", "Players/*.sav"]
min_daemon_version = "1.9.0"
license = "AGPL-3.0-only"
source_url = "https://github.com/joshsymonds/savecraft-client"

[author]
name = "Test Author"
github = "test-author"

[default_paths]
windows = "%LOCALAPPDATA%/Pal/Saved/SaveGames/*/*"
linux = "~/palworld/SaveGames/*/*"
darwin = "~/palworld/SaveGames/*/*"
`

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "plugin.toml"), pluginTOMLSrc)
	writeTestFile(t, filepath.Join(dir, "parser.wasm"), "fake palworld wasm")

	if err := runSingle(dir, "0.1.0"); err != nil {
		t.Fatalf("runSingle: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "client-manifest.json"))
	if err != nil {
		t.Fatalf("read client-manifest.json: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatalf("decode client-manifest.json: %v", err)
	}

	checkString := func(field, want string) {
		t.Helper()
		raw, ok := fields[field]
		if !ok {
			t.Errorf("client-manifest.json missing %q field", field)
			return
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("decode %q: %v", field, err)
		}
		if s != want {
			t.Errorf("%s = %q, want %q", field, s, want)
		}
	}
	checkString("unit", "directory")
	checkString("min_daemon_version", "1.9.0")
	checkString("license", "AGPL-3.0-only")
	checkString("source_url", "https://github.com/joshsymonds/savecraft-client")

	raw, ok := fields["members"]
	if !ok {
		t.Fatal("client-manifest.json missing members field")
	}
	var members []string
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	wantMembers := []string{"Level.sav", "LevelMeta.sav", "Players/*.sav"}
	if len(members) != len(wantMembers) {
		t.Fatalf("members = %v, want %v", members, wantMembers)
	}
	for i, m := range wantMembers {
		if members[i] != m {
			t.Errorf("members[%d] = %q, want %q", i, members[i], m)
		}
	}
}

// TestRunSingle_OmitsDirectoryUnitFieldsWhenUnset confirms the new fields
// are absent from client-manifest.json for a plain file-unit plugin that
// never sets them — the existing byte-for-byte fixtures in TestRunSingle
// already prove this for the exact JSON shape; this test states the
// omitempty contract explicitly and independently.
func TestRunSingle_OmitsDirectoryUnitFieldsWhenUnset(t *testing.T) {
	const pluginTOMLSrc = `game_id = "d2r"
sources = ["wasm"]
icon = "d2r.svg"
name = "Diablo II: Resurrected"
description = "Test plugin"
channel = "stable"
coverage = "full"
file_extensions = [".d2s"]

[author]
name = "Test Author"
github = "test-author"

[default_paths]
windows = "%USERPROFILE%/d2r"
linux = "~/d2r"
darwin = "~/d2r"
`
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "plugin.toml"), pluginTOMLSrc)

	if err := runSingle(dir, "0.1.0"); err != nil {
		t.Fatalf("runSingle: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "client-manifest.json"))
	if err != nil {
		t.Fatalf("read client-manifest.json: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatalf("decode client-manifest.json: %v", err)
	}
	for _, absent := range []string{"unit", "members", "min_daemon_version", "license", "source_url"} {
		if _, ok := fields[absent]; ok {
			t.Errorf("client-manifest.json contains %q field, want absent when unset", absent)
		}
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
