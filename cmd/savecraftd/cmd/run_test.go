package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://install-staging.savecraft.gg")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ServerURL != "https://staging-api.savecraft.gg" {
		t.Errorf("ServerURL = %q, want https://staging-api.savecraft.gg", cfg.ServerURL)
	}
	if cfg.InstallURL != "https://install-staging.savecraft.gg" {
		t.Errorf("InstallURL = %q, want https://install-staging.savecraft.gg", cfg.InstallURL)
	}
}

func TestLoadConfig_EnvVarOverridesDefault(t *testing.T) {
	t.Setenv("SAVECRAFT_SERVER_URL", "https://custom.savecraft.gg")
	t.Setenv("SAVECRAFT_INSTALL_URL", "https://custom-install.savecraft.gg")
	t.Setenv("SAVECRAFT_AUTH_TOKEN", "test-token")

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://install-staging.savecraft.gg")
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

	cfg, err := loadConfig("https://staging-api.savecraft.gg", "https://install-staging.savecraft.gg")
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

func TestAutoRegister_RegistersAndPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/device/register" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		var body struct {
			DeviceName string `json:"device_name"`
		}

		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		json.NewEncoder(rw).Encode(map[string]string{
			"device_uuid":          "test-uuid-1234",
			"token":                "dvt_testtoken",
			"link_code":            "123456",
			"link_code_expires_at": "2026-03-03T12:20:00Z",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	cfg := &appConfig{
		ServerURL: srv.URL,
		Daemon:    daemonConfigDefaults("test-host", "dev"),
	}

	result, err := autoRegister(context.Background(), cfg, envPath)
	if err != nil {
		t.Fatalf("autoRegister: %v", err)
	}

	// Verify config was updated.
	if cfg.AuthToken != "dvt_testtoken" {
		t.Errorf("cfg.AuthToken = %q, want dvt_testtoken", cfg.AuthToken)
	}
	if cfg.Daemon.AuthToken != "dvt_testtoken" {
		t.Errorf("cfg.Daemon.AuthToken = %q, want dvt_testtoken", cfg.Daemon.AuthToken)
	}
	if cfg.Daemon.DeviceUUID != "test-uuid-1234" {
		t.Errorf("cfg.Daemon.DeviceUUID = %q, want test-uuid-1234", cfg.Daemon.DeviceUUID)
	}

	// Verify credentials persisted to env file.
	vars, readErr := envfile.Read(envPath)
	if readErr != nil {
		t.Fatalf("read env: %v", readErr)
	}

	if vars["SAVECRAFT_AUTH_TOKEN"] != "dvt_testtoken" {
		t.Errorf("env SAVECRAFT_AUTH_TOKEN = %q, want dvt_testtoken", vars["SAVECRAFT_AUTH_TOKEN"])
	}
	if vars["SAVECRAFT_DEVICE_UUID"] != "test-uuid-1234" {
		t.Errorf("env SAVECRAFT_DEVICE_UUID = %q, want test-uuid-1234", vars["SAVECRAFT_DEVICE_UUID"])
	}

	// Verify link code returned.
	if result.LinkCode != "123456" {
		t.Errorf("link_code = %q, want 123456", result.LinkCode)
	}
}

func TestAutoRegister_SkipsIfTokenExists(t *testing.T) {
	cfg := &appConfig{
		ServerURL: "https://api.savecraft.gg",
		AuthToken: "dvt_existing",
		Daemon:    daemonConfigDefaults("test-host", "dev"),
	}
	cfg.Daemon.AuthToken = "dvt_existing"

	result, err := autoRegister(context.Background(), cfg, "/nonexistent/path")
	if err != nil {
		t.Fatalf("autoRegister: %v", err)
	}

	if result != nil {
		t.Error("expected nil result when token exists")
	}
}
