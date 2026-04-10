package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		name     string
		template string
		envVars  map[string]string
		want     string
	}{
		{
			name:     "tilde with path",
			template: "~/foo",
			want:     home + "/foo",
		},
		{
			name:     "tilde alone",
			template: "~",
			want:     home,
		},
		{
			name:     "env var expansion",
			template: "%TESTVAR%/bar",
			envVars:  map[string]string{"TESTVAR": "/custom"},
			want:     "/custom/bar",
		},
		{
			name:     "empty string",
			template: "",
			want:     "",
		},
		{
			name:     "absolute path unchanged",
			template: "/usr/local/bin",
			want:     "/usr/local/bin",
		},
		{
			name:     "multiple env vars",
			template: "%HOME_A%/saves/%GAME%",
			envVars:  map[string]string{"HOME_A": "/home/user", "GAME": "d2r"},
			want:     "/home/user/saves/d2r",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			got := expandPath(tt.template)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.template, got, tt.want)
			}
		})
	}
}

func TestHasGlobMeta(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/home/user/saves", false},
		{"/home/user/saves/*", true},
		{"/home/user/saves/[0-9]*", true},
		{"/home/user/saves/save?.sav", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := hasGlobMeta(tt.path); got != tt.want {
			t.Errorf("hasGlobMeta(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestResolveGlob_NoGlob(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{"/saves/d2r": {"Atmus.d2s"}},
	}
	got := resolveGlob(fs, "/saves/d2r", nil)
	if len(got) != 1 || got[0] != "/saves/d2r" {
		t.Errorf("resolveGlob(non-glob) = %v, want [/saves/d2r]", got)
	}
}

func TestResolveGlob_Wildcard(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":       {"12345", "67890", "settings.txt"},
			"/saves/12345": {"EXPEDITION_0.sav"},
			"/saves/67890": {"EXPEDITION_0.sav"},
		},
		files: map[string][]byte{
			"/saves/settings.txt":           []byte("config"),
			"/saves/12345/EXPEDITION_0.sav": []byte("save1"),
			"/saves/67890/EXPEDITION_0.sav": []byte("save2"),
		},
	}
	got := resolveGlob(fs, "/saves/*", nil)
	// Should return only directories, sorted.
	want := []string{"/saves/12345", "/saves/67890"}
	if len(got) != len(want) {
		t.Fatalf("resolveGlob(/saves/*) returned %d results, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveGlob(/saves/*)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveGlob_NoMatch(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{"/saves": {}},
	}
	got := resolveGlob(fs, "/saves/*", nil)
	// Returns original pattern when nothing matches.
	if len(got) != 1 || got[0] != "/saves/*" {
		t.Errorf("resolveGlob(no match) = %v, want [/saves/*]", got)
	}
}

func TestResolveGlob_QuestionMark(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":     {"ab", "ac", "xyz"},
			"/saves/ab":  {"save.sav"},
			"/saves/ac":  {"save.sav"},
			"/saves/xyz": {"save.sav"},
		},
	}
	got := resolveGlob(fs, "/saves/a?", nil)
	want := []string{filepath.Join("/saves", "ab"), filepath.Join("/saves", "ac")}
	if len(got) != len(want) {
		t.Fatalf("resolveGlob(/saves/a?) returned %d results, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveGlob(/saves/a?)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveGlob_BracketPattern(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":       {"alpha", "beta", "gamma"},
			"/saves/alpha": {"save.sav"},
			"/saves/beta":  {"save.sav"},
			"/saves/gamma": {"save.sav"},
		},
	}
	got := resolveGlob(fs, "/saves/[ab]*", nil)
	want := []string{filepath.Join("/saves", "alpha"), filepath.Join("/saves", "beta")}
	if len(got) != len(want) {
		t.Fatalf("resolveGlob(/saves/[ab]*) returned %d results, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveGlob(/saves/[ab]*)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveGlob_NestedGlobReturnsPattern(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{"/saves": {"dir1"}},
	}
	// Nested glob (parent has metachar) should return pattern as-is.
	got := resolveGlob(fs, "/*/saves", nil)
	if len(got) != 1 || got[0] != "/*/saves" {
		t.Errorf("resolveGlob(nested glob) = %v, want [/*/saves]", got)
	}
}

func TestResolveGlob_ExcludeDirs(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":        {"12345", "Backup", "67890"},
			"/saves/12345":  {"EXPEDITION_0.sav"},
			"/saves/Backup": {"EXPEDITION_0.sav"},
			"/saves/67890":  {"EXPEDITION_0.sav"},
		},
	}
	got := resolveGlob(fs, "/saves/*", []string{"Backup"})
	want := []string{"/saves/12345", "/saves/67890"}
	if len(got) != len(want) {
		t.Fatalf("resolveGlob with exclude returned %d results, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveGlob with exclude[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveGlob_ExcludeDirsCaseInsensitive(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":        {"12345", "BACKUP", "backup"},
			"/saves/12345":  {"save.sav"},
			"/saves/BACKUP": {"save.sav"},
			"/saves/backup": {"save.sav"},
		},
	}
	got := resolveGlob(fs, "/saves/*", []string{"Backup"})
	want := []string{"/saves/12345"}
	if len(got) != len(want) {
		t.Fatalf("resolveGlob case-insensitive exclude returned %d results, want %d: %v", len(got), len(want), got)
	}
	if got[0] != want[0] {
		t.Errorf("resolveGlob case-insensitive exclude = %v, want %v", got, want)
	}
}

func TestResolveGlob_EmptyExcludeDirs(t *testing.T) {
	fs := &fakeFS{
		dirs: map[string][]string{
			"/saves":        {"12345", "Backup"},
			"/saves/12345":  {"save.sav"},
			"/saves/Backup": {"save.sav"},
		},
	}
	// Empty exclude list should not filter anything.
	got := resolveGlob(fs, "/saves/*", []string{})
	if len(got) != 2 {
		t.Fatalf("resolveGlob with empty exclude returned %d results, want 2: %v", len(got), got)
	}
}

func TestIsExcludedSave(t *testing.T) {
	tests := []struct {
		name         string
		excludeSaves []string
		want         bool
	}{
		{"Atmus.d2s", []string{"Atmus.d2s"}, true},
		{"Atmus.d2s", []string{"atmus.d2s"}, true},                  // case-insensitive
		{"ATMUS.D2S", []string{"Atmus.d2s"}, true},                  // case-insensitive
		{"Blizzara.d2s", []string{"Atmus.d2s"}, false},              // no match
		{"Atmus.d2s", nil, false},                                   // nil list
		{"Atmus.d2s", []string{}, false},                            // empty list
		{"TrapSin.d2s", []string{"Atmus.d2s", "TrapSin.d2s"}, true}, // multiple entries
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExcludedSave(tt.name, tt.excludeSaves)
			if got != tt.want {
				t.Errorf("isExcludedSave(%q, %v) = %v, want %v", tt.name, tt.excludeSaves, got, tt.want)
			}
		})
	}
}

func TestExpandPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	t.Run("known folder DOCUMENTS produces two candidates", func(t *testing.T) {
		t.Setenv("USERPROFILE", "/Users/TestUser")
		got := expandPaths("%DOCUMENTS%/Paradox Interactive/Stellaris")
		// First candidate: Known Folder resolution (~/Documents on Linux).
		// Second candidate: %USERPROFILE%/Documents fallback.
		if len(got) < 1 {
			t.Fatal("expandPaths returned empty slice")
		}
		if got[0] != home+"/Documents/Paradox Interactive/Stellaris" {
			t.Errorf("expandPaths[0] = %q, want %q", got[0], home+"/Documents/Paradox Interactive/Stellaris")
		}
		if len(got) != 2 {
			t.Fatalf("expandPaths returned %d candidates, want 2", len(got))
		}
		if got[1] != "/Users/TestUser/Documents/Paradox Interactive/Stellaris" {
			t.Errorf("expandPaths[1] = %q, want %q", got[1], "/Users/TestUser/Documents/Paradox Interactive/Stellaris")
		}
	})

	t.Run("known folder deduplicates identical candidates", func(t *testing.T) {
		// Set USERPROFILE to match what resolveKnownFolder returns on Linux.
		t.Setenv("USERPROFILE", home)
		got := expandPaths("%DOCUMENTS%/saves")
		// Both resolve to ~/Documents/saves — should dedup to one.
		if len(got) != 1 {
			t.Errorf("expandPaths returned %d candidates, want 1 (dedup): %v", len(got), got)
		}
		if got[0] != home+"/Documents/saves" {
			t.Errorf("expandPaths[0] = %q, want %q", got[0], home+"/Documents/saves")
		}
	})

	t.Run("known folder LOCALAPPDATA produces two candidates", func(t *testing.T) {
		t.Setenv("USERPROFILE", "/Users/TestUser")
		got := expandPaths("%LOCALAPPDATA%/Game")
		if len(got) != 2 {
			t.Fatalf("expandPaths returned %d candidates, want 2: %v", len(got), got)
		}
		// First: Known Folder resolution (~/.local/share on Linux).
		if got[0] != home+"/.local/share/Game" {
			t.Errorf("expandPaths[0] = %q, want %q", got[0], home+"/.local/share/Game")
		}
		// Second: %USERPROFILE%/AppData/Local fallback.
		if got[1] != "/Users/TestUser/AppData/Local/Game" {
			t.Errorf("expandPaths[1] = %q, want %q", got[1], "/Users/TestUser/AppData/Local/Game")
		}
	})

	t.Run("known folder LOCALAPPDATA_LOW produces two candidates", func(t *testing.T) {
		t.Setenv("USERPROFILE", "/Users/TestUser")
		got := expandPaths("%LOCALAPPDATA_LOW%/Game")
		if len(got) != 2 {
			t.Fatalf("expandPaths returned %d candidates, want 2: %v", len(got), got)
		}
		if got[1] != "/Users/TestUser/AppData/Local/Low/Game" {
			t.Errorf("expandPaths[1] = %q, want %q", got[1], "/Users/TestUser/AppData/Local/Low/Game")
		}
	})

	t.Run("known folder SAVED_GAMES on non-Windows still has fallback", func(t *testing.T) {
		t.Setenv("USERPROFILE", "/Users/TestUser")
		got := expandPaths("%SAVED_GAMES%/Diablo II Resurrected")
		// On Linux, resolveKnownFolder("SAVED_GAMES") errors.
		// Should still return the fallback.
		found := false
		for _, p := range got {
			if p == "/Users/TestUser/Saved Games/Diablo II Resurrected" {
				found = true
			}
		}
		if !found {
			t.Errorf("expandPaths missing fallback candidate: %v", got)
		}
	})

	t.Run("regular env var returns single candidate", func(t *testing.T) {
		t.Setenv("APPDATA", "/home/user/.config")
		got := expandPaths("%APPDATA%/StardewValley/Saves")
		if len(got) != 1 {
			t.Fatalf("expandPaths returned %d candidates, want 1: %v", len(got), got)
		}
		if got[0] != "/home/user/.config/StardewValley/Saves" {
			t.Errorf("expandPaths[0] = %q, want %q", got[0], "/home/user/.config/StardewValley/Saves")
		}
	})

	t.Run("tilde returns single candidate", func(t *testing.T) {
		got := expandPaths("~/saves")
		if len(got) != 1 {
			t.Fatalf("expandPaths returned %d candidates, want 1: %v", len(got), got)
		}
		if got[0] != home+"/saves" {
			t.Errorf("expandPaths[0] = %q, want %q", got[0], home+"/saves")
		}
	})

	t.Run("empty string returns single empty candidate", func(t *testing.T) {
		got := expandPaths("")
		if len(got) != 1 || got[0] != "" {
			t.Errorf("expandPaths('') = %v, want ['']", got)
		}
	})
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"EXPEDITION_0.sav", []string{"EXPEDITION_*"}, true},
		{"EXPEDITION_1.sav", []string{"EXPEDITION_*"}, true},
		{"EnhancedInputUserSettings.sav", []string{"EXPEDITION_*"}, false},
		{"PlatformSaveData.sav", []string{"EXPEDITION_*"}, false},
		{"EXPEDITION_0.sav", []string{"EXPEDITION_*", "SavesContainer*"}, true},
		{"SavesContainer.sav", []string{"EXPEDITION_*", "SavesContainer*"}, true},
		{"whatever.sav", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.name, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesPattern(%q, %v) = %v, want %v", tt.name, tt.patterns, got, tt.want)
			}
		})
	}
}
