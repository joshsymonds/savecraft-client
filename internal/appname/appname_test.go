package appname

import "testing"

func TestBinaryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"savecraft", "savecraft-daemon"},
		{"savecraft-staging", "savecraft-staging-daemon"},
	}

	for _, tc := range tests {
		if got := BinaryName(tc.input); got != tc.want {
			t.Errorf("BinaryName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTitleName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"savecraft", "Savecraft"},
		{"savecraft-staging", "Savecraft-staging"},
		{"myapp", "Myapp"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := TitleName(tc.input); got != tc.want {
			t.Errorf("TitleName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
