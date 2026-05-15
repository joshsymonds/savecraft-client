package version

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.9.0", "0.10.0", false},
		{"1.0", "1.0.0", false},
		{"1.0.1", "1.0", true},
		// Dev versions use 0.0.0-dev.N.SHA format; numeric comparison
		// treats the "-dev" segment as 0, so any release > 0.0.0 wins.
		{"0.1.0", "0.0.0", true},
		{"0.0.1", "0.0.0", true},
		// Empty / garbage compare as 0 — never "newer" (fail-closed
		// anti-rollback relies on this).
		{"", "1.0.0", false},
		{"garbage", "1.0.0", false},
		{"1.0.0", "", true},
	}
	for _, tt := range tests {
		if got := IsNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}
