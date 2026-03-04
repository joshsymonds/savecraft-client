package envfile

import "testing"

func TestTitleName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"savecraft", "Savecraft"},
		{"savecraft-staging", "Savecraft-staging"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := titleName(tc.input); got != tc.want {
			t.Errorf("titleName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
