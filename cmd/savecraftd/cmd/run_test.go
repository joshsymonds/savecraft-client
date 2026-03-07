package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
)

func TestLoadConfig_UsesServerURLDefault(t *testing.T) {
	// Unset SAVECRAFT_SERVER_URL so loadConfig falls back to default.
	t.Setenv("SAVECRAFT_SERVER_URL", "")
	os.Unsetenv("SAVECRAFT_SERVER_URL")

	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://staging-install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ServerURL != "https://staging-api.savecraft.gg" {
		t.Errorf("ServerURL = %q, want https://staging-api.savecraft.gg", cfg.ServerURL)
	}
	if cfg.InstallURL != "https://staging-install.savecraft.gg" {
		t.Errorf("InstallURL = %q, want https://staging-install.savecraft.gg", cfg.InstallURL)
	}
}

func TestLoadConfig_EnvVarOverridesDefault(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "https://custom.savecraft.gg")
	t.Setenv("SAVECRAFT_INSTALL_URL", "https://custom-install.savecraft.gg")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://staging-install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ServerURL != "https://custom.savecraft.gg" {
		t.Errorf("ServerURL = %q, want https://custom.savecraft.gg", cfg.ServerURL)
	}
	if cfg.InstallURL != "https://custom-install.savecraft.gg" {
		t.Errorf("InstallURL = %q, want https://custom-install.savecraft.gg", cfg.InstallURL)
	}
}

func TestLoadConfig_EnvFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	if err := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_SERVER_URL":  "https://from-file.savecraft.gg",
		"SAVECRAFT_INSTALL_URL": "https://install-from-file.savecraft.gg",
		"SAVECRAFT_AUTH_TOKEN":  "file-token",
	}); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Ensure env vars are unset so env file takes effect.
	t.Setenv("SAVECRAFT_SERVER_URL", "")
	os.Unsetenv("SAVECRAFT_SERVER_URL")
	t.Setenv("SAVECRAFT_INSTALL_URL", "")
	os.Unsetenv("SAVECRAFT_INSTALL_URL")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "")
	os.Unsetenv("SAVECRAFT_AUTH_TOKEN")

	loadEnvFileDefaultsFromPath(envPath)

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://staging-install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ServerURL != "https://from-file.savecraft.gg" {
		t.Errorf("ServerURL = %q, want https://from-file.savecraft.gg", cfg.ServerURL)
	}
	if cfg.InstallURL != "https://install-from-file.savecraft.gg" {
		t.Errorf("InstallURL = %q, want https://install-from-file.savecraft.gg", cfg.InstallURL)
	}
}

func TestLoadConfig_FailsWithNoServerURL(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "")
	os.Unsetenv("SAVECRAFT_SERVER_URL")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")

	_, err := loadConfig("", "https://install.savecraft.gg")
	if err == nil {
		t.Fatal("expected error when no server URL available, got nil")
	}
}

func TestLoadConfig_FailsWithNoInstallURL(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "https://api.savecraft.gg")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")
	t.Setenv("SAVECRAFT_INSTALL_URL", "")
	os.Unsetenv("SAVECRAFT_INSTALL_URL")

	_, err := loadConfig("https://api.savecraft.gg", "")
	if err == nil {
		t.Fatal("expected error when no install URL available, got nil")
	}
}

func TestLoadConfig_InstallURLFallsBackToDefault(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "https://api.savecraft.gg")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")
	t.Setenv("SAVECRAFT_INSTALL_URL", "")
	os.Unsetenv("SAVECRAFT_INSTALL_URL")

	cfg, err := loadConfig("", "https://install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.InstallURL != "https://install.savecraft.gg" {
		t.Errorf("InstallURL = %q, want https://install.savecraft.gg", cfg.InstallURL)
	}
}

func TestLoadConfig_AcceptsMissingAuthToken(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "https://api.savecraft.gg")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "")
	os.Unsetenv("SAVECRAFT_AUTH_TOKEN")

	cfg, err := loadConfig("https://api.savecraft.gg", "https://install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty string", cfg.AuthToken)
	}
}

func TestAutoRegister_SkipsIfTokenExists(t *testing.T) {
	cfg := &appConfig{
		ServerURL: "https://api.savecraft.gg",
		AuthToken: "dvt_existing",
		Daemon:    daemonConfigDefaults("test-host", "dev"),
	}
	cfg.Daemon.AuthToken = "dvt_existing"

	result, registered, err := autoRegister(context.Background(), cfg, "/nonexistent/path")
	if err != nil {
		t.Fatalf("autoRegister: %v", err)
	}

	if registered {
		t.Error("expected registered=false when token exists")
	}
	if result != nil {
		t.Error("expected nil result when token exists")
	}
}
