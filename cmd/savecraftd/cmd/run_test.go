package cmd

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
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

func TestAutoRegister_RegistersAndPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/source/register" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		var body struct {
			Hostname string `json:"hostname"`
		}

		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		json.NewEncoder(rw).Encode(map[string]string{
			"source_uuid":          "test-uuid-1234",
			"source_token":         "dvt_testtoken",
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

	result, registered, err := autoRegister(context.Background(), cfg, envPath)
	if err != nil {
		t.Fatalf("autoRegister: %v", err)
	}
	if !registered {
		t.Fatal("expected registered=true for new source")
	}

	// Verify config was updated.
	if cfg.AuthToken != "dvt_testtoken" {
		t.Errorf("cfg.AuthToken = %q, want dvt_testtoken", cfg.AuthToken)
	}
	if cfg.Daemon.AuthToken != "dvt_testtoken" {
		t.Errorf("cfg.Daemon.AuthToken = %q, want dvt_testtoken", cfg.Daemon.AuthToken)
	}
	if cfg.Daemon.SourceUUID != "test-uuid-1234" {
		t.Errorf("cfg.Daemon.SourceUUID = %q, want test-uuid-1234", cfg.Daemon.SourceUUID)
	}

	// Verify credentials persisted to env file.
	vars, readErr := envfile.Read(envPath)
	if readErr != nil {
		t.Fatalf("read env: %v", readErr)
	}

	if vars["SAVECRAFT_AUTH_TOKEN"] != "dvt_testtoken" {
		t.Errorf("env SAVECRAFT_AUTH_TOKEN = %q, want dvt_testtoken", vars["SAVECRAFT_AUTH_TOKEN"])
	}
	if vars["SAVECRAFT_SOURCE_UUID"] != "test-uuid-1234" {
		t.Errorf("env SAVECRAFT_SOURCE_UUID = %q, want test-uuid-1234", vars["SAVECRAFT_SOURCE_UUID"])
	}

	// Verify link code returned.
	if result.LinkCode != "123456" {
		t.Errorf("link_code = %q, want 123456", result.LinkCode)
	}
}

func TestHandleExistingSource_ReRegistersOn404(t *testing.T) {
	// Simulate a server where /status returns 404, then /register succeeds.
	var statusCalls, registerCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/source/status":
			statusCalls++
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusNotFound)
			json.NewEncoder(rw).Encode(map[string]string{"error": "Source not found"})

		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/source/register":
			registerCalls++
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)
			json.NewEncoder(rw).Encode(map[string]string{
				"source_uuid":          "new-uuid-5678",
				"source_token":         "sct_newtoken",
				"link_code":            "999999",
				"link_code_expires_at": "2099-01-01T00:00:00Z",
			})

		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/source/status" && req.Header.Get("Authorization") == "Bearer sct_newtoken":
			// After re-registration, status checks during waitForLink.
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode(map[string]any{"linked": true})

		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	xdgDir := t.TempDir()
	appDir := filepath.Join(xdgDir, "test-app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	envPath := filepath.Join(appDir, "env")

	// Write stale credentials.
	if err := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_AUTH_TOKEN":  "sct_staletoken",
		"SAVECRAFT_SOURCE_UUID": "old-uuid-1234",
	}); err != nil {
		t.Fatalf("write env: %v", err)
	}

	cfg := &appConfig{
		ServerURL: srv.URL,
		AuthToken: "sct_staletoken",
		Daemon:    daemonConfigDefaults("test-host", "dev"),
	}
	cfg.Daemon.AuthToken = "sct_staletoken"

	api := localapi.NewServer("localhost:0", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Point XDG_CONFIG_HOME so envfile.EnvFilePath("test-app") resolves to our temp dir.
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := handleExistingSource(ctx, cfg, "test-app", "https://test.savecraft.gg", api, logger)
	// handleExistingSource calls reRegister which calls handleNewRegistration
	// which calls waitForLink. waitForLink will time out or return an error
	// because the test server doesn't provide the right status endpoint for the
	// new token during the link-polling loop. That's fine — what we care about
	// is that re-registration happened.

	// Verify the re-registration flow was triggered.
	if statusCalls == 0 {
		t.Error("expected at least one status call")
	}

	if registerCalls == 0 {
		t.Error("expected register to be called after 404, but it was not")
	}

	// Verify the config was updated with new credentials.
	if cfg.AuthToken != "sct_newtoken" {
		t.Errorf("cfg.AuthToken = %q, want sct_newtoken", cfg.AuthToken)
	}

	_ = err // waitForLink error is expected in test — we only care about re-registration
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
