package daemon

import (
	"os"
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
