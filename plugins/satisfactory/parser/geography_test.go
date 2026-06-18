package main

import (
	"testing"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func TestParseMapMarkers(t *testing.T) {
	od := &sav.ObjectData{Properties: map[string]any{
		"mMapMarkers": []any{
			map[string]any{
				"Name":     "Steel Production",
				"Location": map[string]any{"X": -107244.0, "Y": -126509.0, "Z": -842.0},
			},
			map[string]any{
				"Name":     "Oil Processing",
				"Location": map[string]any{"X": -242897.0, "Y": 63154.0, "Z": -1500.0},
			},
			// Skipped: no name.
			map[string]any{"Location": map[string]any{"X": 1.0, "Y": 2.0, "Z": 3.0}},
			// Skipped: no location.
			map[string]any{"Name": "Nowhere"},
		},
	}}
	markers := parseMapMarkers(od)
	if len(markers) != 2 {
		t.Fatalf("markers = %d, want 2: %+v", len(markers), markers)
	}
	if markers[0].name != "Steel Production" || markers[0].x != -107244 || markers[0].y != -126509 {
		t.Errorf("marker[0] = %+v", markers[0])
	}
	if markers[1].name != "Oil Processing" {
		t.Errorf("marker[1] = %+v", markers[1])
	}
}

func TestParseMapMarkersAbsent(t *testing.T) {
	// No FGMapManager markers property → empty, no panic.
	if got := parseMapMarkers(&sav.ObjectData{Properties: map[string]any{}}); len(got) != 0 {
		t.Errorf("markers = %v, want empty", got)
	}
}

func TestAreaName(t *testing.T) {
	got := areaName("/Game/FactoryGame/Interface/UI/Minimap/MapAreaPersistenLevel/Area_RockyDesert_2.Area_RockyDesert_2_C")
	if got != "Area_RockyDesert_2" {
		t.Errorf("areaName = %q, want Area_RockyDesert_2", got)
	}
}
