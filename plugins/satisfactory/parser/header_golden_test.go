package main

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// Golden tests against real save files. Ground truth is documented in
// testdata/README.md alongside each fixture's provenance.

func parseFixture(t *testing.T, name string) *sav.Header {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s not present (gitignored fixture — see testdata/README.md for source URL)", name)
	}
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()

	h, err := sav.ParseHeader(f)
	if err != nil {
		t.Fatalf("ParseHeader(%s): %v", name, err)
	}
	return h
}

func TestGoldenEarlyGame(t *testing.T) {
	h := parseFixture(t, "early_game.sav")

	if h.HeaderVersion != 13 {
		t.Errorf("HeaderVersion = %d, want 13", h.HeaderVersion)
	}
	if h.SaveVersion != 46 {
		t.Errorf("SaveVersion = %d, want 46", h.SaveVersion)
	}
	if h.BuildVersion != 368883 {
		t.Errorf("BuildVersion = %d, want 368883", h.BuildVersion)
	}
	if h.SaveName != "" {
		t.Errorf("SaveName = %q, want empty (header v13)", h.SaveName)
	}
	if h.SessionName != "Release" {
		t.Errorf("SessionName = %q, want Release", h.SessionName)
	}
	if h.MapName != "Persistent_Level" {
		t.Errorf("MapName = %q, want Persistent_Level", h.MapName)
	}
	if want := 6*time.Hour + 44*time.Second; h.PlayDuration != want {
		t.Errorf("PlayDuration = %v, want %v", h.PlayDuration, want)
	}
	if y, m, d := h.SaveTime.Date(); y != 2024 || m != time.September || d != 29 {
		t.Errorf("SaveTime = %v, want 2024-09-29", h.SaveTime)
	}
	if h.Modded {
		t.Error("Modded = true, want false")
	}
}

func TestGoldenMegafactory(t *testing.T) {
	h := parseFixture(t, "megafactory.sav")

	if h.HeaderVersion != 14 {
		t.Errorf("HeaderVersion = %d, want 14", h.HeaderVersion)
	}
	if h.SaveVersion != 52 {
		t.Errorf("SaveVersion = %d, want 52", h.SaveVersion)
	}
	if h.BuildVersion != 463028 {
		t.Errorf("BuildVersion = %d, want 463028", h.BuildVersion)
	}
	if h.SaveName != "THP 10.0" {
		t.Errorf("SaveName = %q, want THP 10.0", h.SaveName)
	}
	if h.SessionName != "Leaking Blood Vessel" {
		t.Errorf("SessionName = %q, want Leaking Blood Vessel", h.SessionName)
	}
	if want := 652*time.Hour + 37*time.Minute + 11*time.Second; h.PlayDuration != want {
		t.Errorf("PlayDuration = %v, want %v", h.PlayDuration, want)
	}
	if y, m, d := h.SaveTime.Date(); y != 2025 || m != time.December || d != 26 {
		t.Errorf("SaveTime = %v, want 2025-12-26", h.SaveTime)
	}
}

func TestGoldenCurrent12(t *testing.T) {
	h := parseFixture(t, "current_1_2.sav")

	if h.HeaderVersion != 14 {
		t.Errorf("HeaderVersion = %d, want 14", h.HeaderVersion)
	}
	if h.SaveVersion != 58 {
		t.Errorf("SaveVersion = %d, want 58", h.SaveVersion)
	}
	if h.BuildVersion != 481836 {
		t.Errorf("BuildVersion = %d, want 481836", h.BuildVersion)
	}
	if h.SessionName != "Another 1.2 Baby" {
		t.Errorf("SessionName = %q, want Another 1.2 Baby", h.SessionName)
	}
	if h.PlayDuration <= 0 {
		t.Errorf("PlayDuration = %v, want > 0", h.PlayDuration)
	}
}
