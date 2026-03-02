package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
)

func TestRunVerifyWithPath(t *testing.T) {
	t.Parallel()

	t.Run("valid token returns success", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			if r.URL.Path != "/api/v1/verify" {
				t.Errorf("path = %s, want /api/v1/verify", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer sav_good" {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}
			rw.Header().Set("Content-Type", "application/json")
			rw.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_good",
		}); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", srv.URL})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, srv.URL, envPath)
		}

		if err := cmd.Execute(); err != nil {
			t.Fatalf("verify should succeed: %v\noutput: %s", err, out.String())
		}
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_bad",
		}); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", srv.URL})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, srv.URL, envPath)
		}

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for invalid token, got nil")
		}
	})

	t.Run("missing token returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_SERVER_URL": "https://example.com",
		}); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", "https://example.com"})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, "https://example.com", envPath)
		}

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for missing token, got nil")
		}
	})

	t.Run("missing env file returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "nonexistent", "env")

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", "https://example.com"})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, "https://example.com", envPath)
		}

		err := cmd.Execute()
		// envfile.Read returns empty map for missing file, so this hits
		// the "no auth token found" path.
		if err == nil {
			t.Fatal("expected error for missing env file, got nil")
		}
	})

	t.Run("server error returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_good",
		}); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", srv.URL})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, srv.URL, envPath)
		}

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for server 500, got nil")
		}
	})

	t.Run("server unreachable returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_good",
		}); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cmd := buildVerifyCommand()
		cmd.SetArgs([]string{"--server", "http://127.0.0.1:1"})

		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		cmd.RunE = func(c *cobra.Command, _ []string) error {
			return runVerifyWithPath(c, "http://127.0.0.1:1", envPath)
		}

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for unreachable server, got nil")
		}
	})
}
