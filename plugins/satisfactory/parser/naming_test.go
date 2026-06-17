package main

import "testing"

func TestCentroid(t *testing.T) {
	c := centroid([][3]float32{{0, 0, 0}, {10, 20, 30}, {20, 40, 0}})
	if c != [3]float32{10, 20, 10} {
		t.Errorf("centroid = %v, want {10 20 10}", c)
	}
	if got := centroid(nil); got != [3]float32{0, 0, 0} {
		t.Errorf("empty centroid = %v, want zero", got)
	}
}

func TestNearestMarker(t *testing.T) {
	markers := []mapMarker{
		{name: "Far", x: 100000, y: 0},
		{name: "Near", x: 1000, y: 0},
		{name: "Mid", x: 50000, y: 0},
	}
	m, dist, ok := nearestMarker(0, 0, markers)
	if !ok || m.name != "Near" {
		t.Fatalf("nearest = %q ok=%v, want Near", m.name, ok)
	}
	if dist < 999 || dist > 1001 {
		t.Errorf("dist = %f, want ~1000", dist)
	}

	if _, _, ok := nearestMarker(0, 0, nil); ok {
		t.Error("no markers should give ok=false")
	}
}

func TestNearestMarkerTieBreak(t *testing.T) {
	// Two markers equidistant from the origin → stable lexicographic pick.
	markers := []mapMarker{
		{name: "Zulu", x: 1000, y: 0},
		{name: "Alpha", x: -1000, y: 0},
	}
	m, _, _ := nearestMarker(0, 0, markers)
	if m.name != "Alpha" {
		t.Errorf("tie-break = %q, want Alpha", m.name)
	}
}

func TestRegionNameNearMarker(t *testing.T) {
	markers := []mapMarker{{name: "Steel Production", x: 1000, y: 1000}}
	if got := regionName(0, 0, markers); got != "near 'Steel Production'" {
		t.Errorf("regionName = %q, want near 'Steel Production'", got)
	}
}

func TestRegionNameMarkerTooFar(t *testing.T) {
	// Marker beyond markerNearRadius → synthetic, not "near".
	markers := []mapMarker{{name: "Steel Production", x: markerNearRadius + 1, y: 0}}
	got := regionName(0, 0, markers)
	if got == "near 'Steel Production'" {
		t.Errorf("marker beyond radius should not be 'near': %q", got)
	}
	if got != "map center" {
		t.Errorf("origin synthetic = %q, want map center", got)
	}
}

func TestRegionNameNoMarkers(t *testing.T) {
	got := regionName(-250000, 130000, nil)
	if got != "SW sector (-2.5km, 1.3km)" {
		t.Errorf("regionName = %q, want SW sector (-2.5km, 1.3km)", got)
	}
	// Deterministic.
	if regionName(-250000, 130000, nil) != got {
		t.Error("regionName not deterministic")
	}
}

func TestSectorOf(t *testing.T) {
	cases := map[[2]float64]string{
		{0, 0}:         "map center",
		{0, -1000}:     "N",
		{1000, 0}:      "E",
		{0, 1000}:      "S",
		{-1000, 0}:     "W",
		{1000, -1000}:  "NE",
		{1000, 1000}:   "SE",
		{-1000, 1000}:  "SW",
		{-1000, -1000}: "NW",
	}
	for in, want := range cases {
		if got := sectorOf(in[0], in[1]); got != want {
			t.Errorf("sectorOf(%v) = %q, want %q", in, got, want)
		}
	}
}
