package main

import "testing"

func TestBuildIdentity(t *testing.T) {
	tests := []struct {
		name            string
		gs              *GameState
		wantSaveName    string
		wantDisplayName string
	}{
		{
			name:            "screen name set",
			gs:              &GameState{DisplayName: "Veraticus#12345", PlayerID: "client-abc"},
			wantSaveName:    "player",
			wantDisplayName: "Veraticus#12345",
		},
		{
			name:            "only client id set",
			gs:              &GameState{DisplayName: "", PlayerID: "client-abc"},
			wantSaveName:    "player",
			wantDisplayName: "client-abc",
		},
		{
			name:            "neither set",
			gs:              &GameState{DisplayName: "", PlayerID: ""},
			wantSaveName:    "player",
			wantDisplayName: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveName, displayName := buildIdentity(tt.gs)
			if saveName != tt.wantSaveName {
				t.Errorf("saveName = %q, want %q", saveName, tt.wantSaveName)
			}
			if displayName != tt.wantDisplayName {
				t.Errorf("displayName = %q, want %q", displayName, tt.wantDisplayName)
			}
		})
	}
}
