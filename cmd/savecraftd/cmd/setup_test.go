package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

func newWorkingSetupDeps() setupDeps {
	return setupDeps{
		installService: func() error { return nil },
		verifyToken: func(_ context.Context, _, _ string) error {
			return nil
		},
		register: func(_ context.Context, _, _ string) (*registerResult, error) {
			return &registerResult{
				SourceUUID:        "source_test123",
				Token:             "sct_test123",
				LinkCode:          "123456",
				LinkCodeExpiresAt: time.Now().Add(20 * time.Minute).Format(time.RFC3339),
			}, nil
		},
		writeEnv:    func(_ string, _ map[string]string) error { return nil },
		removeFile:  func(_ string) error { return nil },
		startDaemon: func() error { return nil },
		startTray:   func(_ string) error { return nil },
		boot: func(_ context.Context) (*localapi.BootResponse, error) {
			return &localapi.BootResponse{State: localapi.StateRunning}, nil
		},
		link: func(_ context.Context) (*localapi.LinkResponse, int, error) {
			return &localapi.LinkResponse{
				LinkCode: "123456",
				LinkURL:  "https://savecraft.gg/link/123456",
			}, http.StatusOK, nil
		},
		sleep: func(_ time.Duration) {},
	}
}

func newTestSetupConfig(output *bytes.Buffer) setupConfig {
	return setupConfig{
		appName:     "savecraft-test",
		serverURL:   "https://api.savecraft.gg",
		statusPort:  "9182",
		frontendURL: "https://savecraft.gg",
		envPath:     "/tmp/test-env",
		authToken:   "",
		hostname:    "test-host",
		trayPath:    "",
		output:      output,
	}
}

func TestRunSetup(t *testing.T) {
	tests := []struct {
		name       string
		cfg        func(*setupConfig)
		deps       func(*setupDeps)
		wantErr    string
		wantOutput []string
	}{
		{
			name: "fresh install registers and shows link code",
			wantOutput: []string{
				"[1/4] Registering autostart",
				"none found, registering",
				"[2/4] Registered",
				"[3/4] Starting daemon",
				"[4/4] Waiting for daemon",
				"Link code: 123456",
				"https://savecraft.gg/link/123456",
			},
		},
		{
			name: "valid credentials skip registration and show already linked",
			cfg: func(c *setupConfig) {
				c.authToken = "sct_valid"
			},
			deps: func(d *setupDeps) {
				d.link = func(_ context.Context) (*localapi.LinkResponse, int, error) {
					return &localapi.LinkResponse{
						Error: "source was already registered",
					}, http.StatusNotFound, nil
				}
			},
			wantOutput: []string{
				"Checking credentials... valid",
				"Already linked to your account",
			},
		},
		{
			name: "stale credentials re-register",
			cfg: func(c *setupConfig) {
				c.authToken = "sct_stale"
			},
			deps: func(d *setupDeps) {
				d.verifyToken = func(_ context.Context, _, _ string) error {
					return fmt.Errorf("token rejected (HTTP 401)")
				}
			},
			wantOutput: []string{
				"expired, re-registering",
				"[2/4] Registered",
				"Link code: 123456",
			},
		},
		{
			name: "daemon error state returns error",
			deps: func(d *setupDeps) {
				d.boot = func(_ context.Context) (*localapi.BootResponse, error) {
					return &localapi.BootResponse{
						State: localapi.StateError,
						Error: "WebSocket connection refused",
					}, nil
				}
			},
			wantErr: "daemon error: WebSocket connection refused",
		},
		{
			name: "timeout with daemon not responding",
			deps: func(d *setupDeps) {
				d.boot = func(_ context.Context) (*localapi.BootResponse, error) {
					return nil, fmt.Errorf("connection refused")
				}
				d.link = func(_ context.Context) (*localapi.LinkResponse, int, error) {
					return nil, 0, fmt.Errorf("connection refused")
				}
			},
			wantErr: "daemon not responding after 30s",
		},
		{
			name: "timeout with daemon running but no link code",
			deps: func(d *setupDeps) {
				d.link = func(_ context.Context) (*localapi.LinkResponse, int, error) {
					return &localapi.LinkResponse{
						Error: "source not yet registered",
						State: localapi.StateRegistering,
					}, http.StatusServiceUnavailable, nil
				}
			},
			wantErr: "link code not available after 30s",
		},
		{
			name: "registration failure shows network error",
			deps: func(d *setupDeps) {
				d.register = func(_ context.Context, _, _ string) (*registerResult, error) {
					return nil, fmt.Errorf("dial wss://api.savecraft.gg/ws/register: connection refused")
				}
			},
			wantErr: "could not reach savecraft.gg",
		},
		{
			name: "autostart failure returns error",
			deps: func(d *setupDeps) {
				d.installService = func() error {
					return fmt.Errorf("access denied: run as administrator")
				}
			},
			wantErr: "register autostart",
		},
		{
			name: "start daemon failure returns error",
			deps: func(d *setupDeps) {
				d.startDaemon = func() error {
					return fmt.Errorf("binary not found")
				}
			},
			wantErr: "start daemon",
		},
		{
			name: "credential save failure returns error",
			deps: func(d *setupDeps) {
				d.writeEnv = func(_ string, _ map[string]string) error {
					return fmt.Errorf("permission denied")
				}
			},
			wantErr: "save credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := newTestSetupConfig(&buf)
			deps := newWorkingSetupDeps()

			if tt.cfg != nil {
				tt.cfg(&cfg)
			}
			if tt.deps != nil {
				tt.deps(&deps)
			}

			err := runSetup(context.Background(), cfg, deps)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, want := range tt.wantOutput {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
				}
			}
		})
	}
}

func TestSetupRemovesStaleCredentials(t *testing.T) {
	var buf bytes.Buffer
	cfg := newTestSetupConfig(&buf)
	cfg.authToken = "sct_stale"
	cfg.envPath = "/specific/env/path"

	deps := newWorkingSetupDeps()
	deps.verifyToken = func(_ context.Context, _, _ string) error {
		return fmt.Errorf("rejected")
	}

	var removedPath string
	deps.removeFile = func(path string) error {
		removedPath = path
		return nil
	}

	var writtenPath string
	var writtenVars map[string]string
	deps.writeEnv = func(path string, vars map[string]string) error {
		writtenPath = path
		writtenVars = vars
		return nil
	}

	if err := runSetup(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if removedPath != "/specific/env/path" {
		t.Errorf("expected stale env removed at /specific/env/path, got %q", removedPath)
	}

	if writtenPath != "/specific/env/path" {
		t.Errorf("expected new env written to /specific/env/path, got %q", writtenPath)
	}

	if writtenVars["SAVECRAFT_AUTH_TOKEN"] != "sct_test123" {
		t.Errorf("expected token sct_test123, got %q", writtenVars["SAVECRAFT_AUTH_TOKEN"])
	}

	if writtenVars["SAVECRAFT_SOURCE_UUID"] != "source_test123" {
		t.Errorf("expected source UUID source_test123, got %q", writtenVars["SAVECRAFT_SOURCE_UUID"])
	}
}

func TestSetupValidCredentialsSkipsRegistration(t *testing.T) {
	var buf bytes.Buffer
	cfg := newTestSetupConfig(&buf)
	cfg.authToken = "sct_valid"

	deps := newWorkingSetupDeps()
	deps.link = func(_ context.Context) (*localapi.LinkResponse, int, error) {
		return &localapi.LinkResponse{Error: "source was already registered"}, http.StatusNotFound, nil
	}

	registerCalled := false
	deps.register = func(_ context.Context, _, _ string) (*registerResult, error) {
		registerCalled = true
		return nil, fmt.Errorf("should not be called")
	}

	writeEnvCalled := false
	deps.writeEnv = func(_ string, _ map[string]string) error {
		writeEnvCalled = true
		return nil
	}

	if err := runSetup(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if registerCalled {
		t.Error("register should not be called when credentials are valid")
	}

	if writeEnvCalled {
		t.Error("writeEnv should not be called when credentials are valid")
	}
}
