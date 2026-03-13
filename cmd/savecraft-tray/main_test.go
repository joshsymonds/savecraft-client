package main

import "testing"

func TestParseLinkArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode string
		wantURL  string
	}{
		{
			name: "no flags",
			args: nil,
		},
		{
			name: "space-separated flags",
			args: []string{
				"--link-code", "ABC-123",
				"--link-url", "https://example.com/link/ABC-123",
			},
			wantCode: "ABC-123",
			wantURL:  "https://example.com/link/ABC-123",
		},
		{
			name: "equals-separated flags",
			args: []string{
				"--link-code=ABC-123",
				"--link-url=https://example.com/link/ABC-123",
			},
			wantCode: "ABC-123",
			wantURL:  "https://example.com/link/ABC-123",
		},
		{
			name:     "only link-code",
			args:     []string{"--link-code", "ABC-123"},
			wantCode: "ABC-123",
		},
		{
			name:    "only link-url",
			args:    []string{"--link-url", "https://example.com"},
			wantURL: "https://example.com",
		},
		{
			name: "link-code at end without value",
			args: []string{"--link-code"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, url := parseLinkArgs(tt.args)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}
