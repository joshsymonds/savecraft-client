package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/envfile"
)

func TestLoadConfig_UsesServerURLDefault(t *testing.T) {
	// Unset SAVECRAFT_SERVER_URL so loadConfig falls back to default.
	t.Setenv(envServerURL, "")
	os.Unsetenv(envServerURL)

	t.Setenv(envAuthToken, "test-token")

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
	t.Setenv(envServerURL, "https://custom.savecraft.gg")
	t.Setenv("SAVECRAFT_INSTALL_URL", "https://custom-install.savecraft.gg")
	t.Setenv(envAuthToken, "test-token")

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
		envServerURL:            "https://from-file.savecraft.gg",
		"SAVECRAFT_INSTALL_URL": "https://install-from-file.savecraft.gg",
		envAuthToken:            "file-token",
	}); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Ensure env vars are unset so env file takes effect.
	t.Setenv(envServerURL, "")
	os.Unsetenv(envServerURL)
	t.Setenv("SAVECRAFT_INSTALL_URL", "")
	os.Unsetenv("SAVECRAFT_INSTALL_URL")
	t.Setenv(envAuthToken, "")
	os.Unsetenv(envAuthToken)

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
	t.Setenv(envServerURL, "")
	os.Unsetenv(envServerURL)
	t.Setenv(envAuthToken, "test-token")

	_, err := loadConfig("", "https://install.savecraft.gg")
	if err == nil {
		t.Fatal("expected error when no server URL available, got nil")
	}
}

func TestLoadConfig_FailsWithNoInstallURL(t *testing.T) {
	t.Setenv(envServerURL, "https://api.savecraft.gg")
	t.Setenv(envAuthToken, "test-token")
	t.Setenv("SAVECRAFT_INSTALL_URL", "")
	os.Unsetenv("SAVECRAFT_INSTALL_URL")

	_, err := loadConfig("https://api.savecraft.gg", "")
	if err == nil {
		t.Fatal("expected error when no install URL available, got nil")
	}
}

func TestLoadConfig_InstallURLFallsBackToDefault(t *testing.T) {
	t.Setenv(envServerURL, "https://api.savecraft.gg")
	t.Setenv(envAuthToken, "test-token")
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
	t.Setenv(envServerURL, "https://api.savecraft.gg")
	t.Setenv(envAuthToken, "")
	os.Unsetenv(envAuthToken)

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

// -- Server/install URL scheme pinning (finding 4.2 / epic R7) ---------------

func TestRequireSecureURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		dev     bool
		wantErr bool
	}{
		{"https ok", "https://api.savecraft.gg", false, false},
		{"wss ok", "wss://api.savecraft.gg/ws", false, false},
		{"http rejected no dev", "http://api.savecraft.gg", false, true},
		{"http remote rejected even in dev", "http://evil.com", true, true},
		{"ws remote rejected even in dev", "ws://10.0.0.5:8787", true, true},
		{"http loopback ok in dev", "http://localhost:8787", true, false},
		{"http 127.0.0.1 ok in dev", "http://127.0.0.1:8787", true, false},
		{"http ::1 ok in dev", "http://[::1]:8787", true, false},
		{"http loopback rejected without dev", "http://localhost:8787", false, true},
		{"look-alike host rejected", "http://localhost.evil.com", true, true},
		{"empty rejected", "", false, true},
		{"garbage rejected", "://nonsense", false, true},
		{"no host rejected", "https://", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireSecureURL(tt.rawURL, tt.dev)
			if tt.wantErr && err == nil {
				t.Errorf("requireSecureURL(%q, dev=%v) = nil, want error", tt.rawURL, tt.dev)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("requireSecureURL(%q, dev=%v) = %v, want nil", tt.rawURL, tt.dev, err)
			}
		})
	}
}

func TestLoadConfig_RejectsPlaintextServerURL(t *testing.T) {
	t.Setenv(envServerURL, "http://api.savecraft.gg")
	t.Setenv(envAuthToken, "test-token")
	t.Setenv("SAVECRAFT_DEV", "")
	os.Unsetenv("SAVECRAFT_DEV")

	_, err := loadConfig("https://api.savecraft.gg", "https://install.savecraft.gg")
	if err == nil {
		t.Fatal("expected loadConfig to reject a plaintext server URL")
	}
}

func TestLoadConfig_RejectsPlaintextInstallURL(t *testing.T) {
	t.Setenv(envServerURL, "https://api.savecraft.gg")
	t.Setenv("SAVECRAFT_INSTALL_URL", "http://install.savecraft.gg")
	t.Setenv(envAuthToken, "test-token")
	t.Setenv("SAVECRAFT_DEV", "")
	os.Unsetenv("SAVECRAFT_DEV")

	_, err := loadConfig("https://api.savecraft.gg", "https://install.savecraft.gg")
	if err == nil {
		t.Fatal("expected loadConfig to reject a plaintext install URL")
	}
}

func TestLoadConfig_AllowsLoopbackPlaintextInDevMode(t *testing.T) {
	t.Setenv(envServerURL, "http://localhost:8787")
	t.Setenv("SAVECRAFT_INSTALL_URL", "http://127.0.0.1:8788")
	t.Setenv(envAuthToken, "test-token")
	t.Setenv("SAVECRAFT_DEV", "1")

	cfg, err := loadConfig("https://api.savecraft.gg", "https://install.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig with dev loopback: %v", err)
	}
	if cfg.ServerURL != "http://localhost:8787" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
}
