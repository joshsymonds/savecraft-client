package envfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
)

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("reads key=value pairs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		if err := os.WriteFile(path, []byte("FOO=bar\nBAZ=qux\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		vars, err := envfile.Read(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vars["FOO"] != "bar" {
			t.Errorf("FOO = %q, want %q", vars["FOO"], "bar")
		}
		if vars["BAZ"] != "qux" {
			t.Errorf("BAZ = %q, want %q", vars["BAZ"], "qux")
		}
	})

	t.Run("skips comments and blank lines", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		if err := os.WriteFile(path, []byte("# comment\n\nFOO=bar\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		vars, err := envfile.Read(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(vars) != 1 {
			t.Errorf("got %d vars, want 1", len(vars))
		}
	})

	t.Run("returns empty map for missing file", func(t *testing.T) {
		t.Parallel()

		vars, err := envfile.Read("/nonexistent/path/env")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(vars) != 0 {
			t.Errorf("got %d vars, want 0", len(vars))
		}
	})

	t.Run("handles values with equals signs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		if err := os.WriteFile(path, []byte("FOO=bar=baz\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		vars, err := envfile.Read(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vars["FOO"] != "bar=baz" {
			t.Errorf("FOO = %q, want %q", vars["FOO"], "bar=baz")
		}
	})

	t.Run("skips lines without equals sign", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		if err := os.WriteFile(path, []byte("NOEQUALS\nFOO=bar\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		vars, err := envfile.Read(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(vars) != 1 {
			t.Errorf("got %d vars, want 1", len(vars))
		}
		if vars["FOO"] != "bar" {
			t.Errorf("FOO = %q, want %q", vars["FOO"], "bar")
		}
	})
}

func TestWrite(t *testing.T) {
	t.Parallel()

	t.Run("writes key=value pairs and reads them back", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		err := envfile.Write(path, map[string]string{
			"SAVECRAFT_AUTH_TOKEN": "sav_abc123",
			"SAVECRAFT_SERVER_URL": "https://api.savecraft.gg",
		})
		if err != nil {
			t.Fatalf("write: %v", err)
		}

		vars, err := envfile.Read(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if vars["SAVECRAFT_AUTH_TOKEN"] != "sav_abc123" {
			t.Errorf("token = %q, want %q", vars["SAVECRAFT_AUTH_TOKEN"], "sav_abc123")
		}
		if vars["SAVECRAFT_SERVER_URL"] != "https://api.savecraft.gg" {
			t.Errorf("url = %q, want %q", vars["SAVECRAFT_SERVER_URL"], "https://api.savecraft.gg")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "deep", "env")

		err := envfile.Write(path, map[string]string{"FOO": "bar"})
		if err != nil {
			t.Fatalf("write: %v", err)
		}

		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("file not created: %v", statErr)
		}
	})

	t.Run("sets restrictive permissions", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "env")

		err := envfile.Write(path, map[string]string{"TOKEN": "secret"})
		if err != nil {
			t.Fatalf("write: %v", err)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat: %v", statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("permissions = %o, want 600", info.Mode().Perm())
		}
	})
}

func TestConfigDir(t *testing.T) {
	t.Parallel()

	dir := envfile.ConfigDir()
	if dir == "" {
		t.Error("ConfigDir returned empty string")
	}
}

func TestConfigDirXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	dir := envfile.ConfigDir()
	if dir != "/custom/config/savecraft" {
		t.Errorf("ConfigDir = %q, want /custom/config/savecraft", dir)
	}
}

func TestEnvFilePath(t *testing.T) {
	t.Parallel()

	path := envfile.EnvFilePath()
	if path == "" {
		t.Error("EnvFilePath returned empty string")
	}
	if filepath.Base(path) != "env" {
		t.Errorf("basename = %q, want %q", filepath.Base(path), "env")
	}
}
