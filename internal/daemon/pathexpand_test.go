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
	got := resolveGlob(fs, "/saves/d2r")
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
	got := resolveGlob(fs, "/saves/*")
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
	got := resolveGlob(fs, "/saves/*")
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
	got := resolveGlob(fs, "/saves/a?")
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
	got := resolveGlob(fs, "/saves/[ab]*")
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
	got := resolveGlob(fs, "/*/saves")
	if len(got) != 1 || got[0] != "/*/saves" {
		t.Errorf("resolveGlob(nested glob) = %v, want [/*/saves]", got)
	}
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
