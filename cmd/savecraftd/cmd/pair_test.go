package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
)

func TestRunPairWithPath(t *testing.T) {
	t.Parallel()

	t.Run("successful pairing writes env file", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode(map[string]string{
				"token":     "sav_paired123",
				"serverUrl": "https://api.savecraft.gg",
			})
		}))
		defer srv.Close()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		pairCmd := buildPairCommand("savecraft")
		pairCmd.SetArgs([]string{"--server", srv.URL})
		pairCmd.SetIn(bytes.NewBufferString("123456\n"))

		var out bytes.Buffer
		pairCmd.SetOut(&out)
		pairCmd.SetErr(&out)

		// Override RunE to use test env path.
		pairCmd.RunE = func(cmd *cobra.Command, _ []string) error {
			return runPairWithPath(cmd, srv.URL, false, envPath)
		}

		err := pairCmd.Execute()
		if err != nil {
			t.Fatalf("pair: %v\noutput: %s", err, out.String())
		}

		vars, readErr := envfile.Read(envPath)
		if readErr != nil {
			t.Fatalf("read env: %v", readErr)
		}

		if vars["SAVECRAFT_AUTH_TOKEN"] != "sav_paired123" {
			t.Errorf("token = %q, want sav_paired123", vars["SAVECRAFT_AUTH_TOKEN"])
		}

		if vars["SAVECRAFT_SERVER_URL"] != "https://api.savecraft.gg" {
			t.Errorf("url = %q, want https://api.savecraft.gg", vars["SAVECRAFT_SERVER_URL"])
		}
	})

	t.Run("refuses if already paired without force", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_existing",
		}); err != nil {
			t.Fatalf("write: %v", err)
		}

		pairCmd := buildPairCommand("savecraft")
		pairCmd.SetArgs([]string{"--server", "https://example.com"})
		pairCmd.SetIn(bytes.NewBufferString("123456\n"))

		var out bytes.Buffer
		pairCmd.SetOut(&out)
		pairCmd.SetErr(&out)

		pairCmd.RunE = func(cmd *cobra.Command, _ []string) error {
			return runPairWithPath(cmd, "https://example.com", false, envPath)
		}

		err := pairCmd.Execute()
		if err == nil {
			t.Fatal("expected error for already paired, got nil")
		}
	})

	t.Run("force flag overwrites existing credentials", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode(map[string]string{
				"token":     "sav_new_token",
				"serverUrl": "https://new.savecraft.gg",
			})
		}))
		defer srv.Close()

		dir := t.TempDir()
		envPath := filepath.Join(dir, "env")

		if err := envfile.Write(envPath, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_old",
		}); err != nil {
			t.Fatalf("write: %v", err)
		}

		pairCmd := buildPairCommand("savecraft")
		pairCmd.SetArgs([]string{"--server", srv.URL, "--force"})
		pairCmd.SetIn(bytes.NewBufferString("123456\n"))

		var out bytes.Buffer
		pairCmd.SetOut(&out)
		pairCmd.SetErr(&out)

		pairCmd.RunE = func(cmd *cobra.Command, _ []string) error {
			return runPairWithPath(cmd, srv.URL, true, envPath)
		}

		err := pairCmd.Execute()
		if err != nil {
			t.Fatalf("pair --force: %v\noutput: %s", err, out.String())
		}

		vars, readErr := envfile.Read(envPath)
		if readErr != nil {
			t.Fatalf("read: %v", readErr)
		}

		if vars["SAVECRAFT_AUTH_TOKEN"] != "sav_new_token" {
			t.Errorf("token = %q, want sav_new_token", vars["SAVECRAFT_AUTH_TOKEN"])
		}
	})
}

func TestPromptForCodeFromReader(t *testing.T) {
	t.Parallel()

	t.Run("reads valid 6-digit code", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		code, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString("654321\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if code != "654321" {
			t.Errorf("code = %q, want 654321", code)
		}
	})

	t.Run("rejects non-numeric input", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		_, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString("abcdef\n"))
		if err == nil {
			t.Fatal("expected error for non-numeric input, got nil")
		}
	})

	t.Run("rejects too-short code", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		_, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString("123\n"))
		if err == nil {
			t.Fatal("expected error for short code, got nil")
		}
	})

	t.Run("rejects empty input", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		_, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString(""))
		if err == nil {
			t.Fatal("expected error for empty input, got nil")
		}
	})

	t.Run("accepts code with space separator", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		code, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString("298 663\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if code != "298663" {
			t.Errorf("code = %q, want 298663", code)
		}
	})

	t.Run("accepts code with multiple spaces", func(t *testing.T) {
		t.Parallel()

		pairCmd := buildPairCommand("savecraft")
		var out bytes.Buffer
		pairCmd.SetOut(&out)

		code, err := promptForCodeFromReader(pairCmd, bytes.NewBufferString("2 9 8 6 6 3\n"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if code != "298663" {
			t.Errorf("code = %q, want 298663", code)
		}
	})
}

func TestLoadEnvFileDefaultsFromPath_SetsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	// t.Setenv registers cleanup to restore the original value (unset).
	// os.Unsetenv then puts the var into the "truly unset" state the test needs.
	// This is intentional — t.Setenv's cleanup still correctly restores original state.
	t.Setenv("SAVECRAFT_ENVTEST_A", "")
	os.Unsetenv("SAVECRAFT_ENVTEST_A")

	if err := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_ENVTEST_A": "from_file",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	loadEnvFileDefaultsFromPath(envPath)

	got := os.Getenv("SAVECRAFT_ENVTEST_A")
	if got != "from_file" {
		t.Errorf("SAVECRAFT_ENVTEST_A = %q, want from_file", got)
	}
}

func TestLoadEnvFileDefaultsFromPath_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	if err := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_ENVTEST_B": "from_file",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("SAVECRAFT_ENVTEST_B", "already_set")

	loadEnvFileDefaultsFromPath(envPath)

	got := os.Getenv("SAVECRAFT_ENVTEST_B")
	if got != "already_set" {
		t.Errorf("SAVECRAFT_ENVTEST_B = %q, want already_set", got)
	}
}

func TestLoadEnvFileDefaultsFromPath_MissingFile(t *testing.T) {
	t.Parallel()

	// Should not panic or error.
	loadEnvFileDefaultsFromPath("/nonexistent/path/env")
}
