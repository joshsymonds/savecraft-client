package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/localapi"
	"github.com/joshsymonds/savecraft.gg/internal/svcmgr"
)

// setupDeps holds injectable dependencies for the setup command.
// Tests replace these with fakes; production uses real implementations.
type setupDeps struct {
	installService func() error
	verifyToken    func(ctx context.Context, serverURL, token string) error
	register       func(ctx context.Context, serverURL, hostname string) (*registerResult, error)
	writeEnv       func(path string, vars map[string]string) error
	removeFile     func(path string) error
	startDaemon    func() error
	startTray      func(trayPath string) error
	boot           func(ctx context.Context) (*localapi.BootResponse, error)
	link           func(ctx context.Context) (*localapi.LinkResponse, int, error)
	sleep          func(d time.Duration)
}

// setupConfig holds resolved configuration for the setup command.
type setupConfig struct {
	appName     string
	serverURL   string
	statusPort  string
	frontendURL string
	envPath     string
	authToken   string // from env file, may be empty
	hostname    string
	trayPath    string // empty if tray binary not found
	output      io.Writer
}

// setupPollSchedule returns the backoff intervals for polling the daemon local API.
// Total: ~26.5s (500ms + 1s + 1s + 12×2s).
func setupPollSchedule() []time.Duration {
	return []time.Duration{
		500 * time.Millisecond,
		time.Second, time.Second,
		2 * time.Second, 2 * time.Second, 2 * time.Second,
		2 * time.Second, 2 * time.Second, 2 * time.Second,
		2 * time.Second, 2 * time.Second, 2 * time.Second,
		2 * time.Second, 2 * time.Second, 2 * time.Second,
	}
}

func buildSetupCommand(serverURL, appName, statusPort, frontendURL string) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Install, register, and start the daemon (idempotent)",
		Long: `Performs the full post-download installation lifecycle:

1. Registers autostart (OS service / registry)
2. Validates existing credentials or registers a new source
3. Starts the daemon and tray app
4. Waits for link code and displays it

Safe to run multiple times — reuses valid credentials and recovers
from stale state automatically.`,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loadEnvFileDefaults(appName)

			resolvedServerURL := os.Getenv("SAVECRAFT_SERVER_URL")
			if resolvedServerURL == "" {
				resolvedServerURL = serverURL
			}

			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("get hostname: %w", err)
			}
			if envID := os.Getenv("SAVECRAFT_SOURCE_ID"); envID != "" {
				hostname = envID
			}

			exePath, exeErr := os.Executable()
			if exeErr != nil {
				return fmt.Errorf("get executable path: %w", exeErr)
			}

			trayPath := filepath.Join(filepath.Dir(exePath), appName+"-tray"+trayBinaryExt())
			if _, statErr := os.Stat(trayPath); statErr != nil {
				trayPath = ""
			}

			svcCfg := svcmgr.Config{
				Name:        appName + "-daemon",
				DisplayName: "Savecraft Daemon",
				Description: "Syncs game saves to the cloud via Savecraft",
				AppName:     appName,
			}

			client := localapi.NewClient("http://localhost:" + statusPort)

			cfg := setupConfig{
				appName:     appName,
				serverURL:   resolvedServerURL,
				statusPort:  statusPort,
				frontendURL: frontendURL,
				envPath:     envfile.EnvFilePath(appName),
				authToken:   os.Getenv("SAVECRAFT_AUTH_TOKEN"),
				hostname:    hostname,
				trayPath:    trayPath,
				output:      cmd.ErrOrStderr(),
			}

			deps := setupDeps{
				installService: func() error { return svcmgr.Control(svcCfg, "install") },
				verifyToken:    httpVerifyToken,
				register:       wsRegister,
				writeEnv:       envfile.Write,
				removeFile:     os.Remove,
				startDaemon:    func() error { return svcmgr.Control(svcCfg, "start") },
				startTray:      startTrayProcess,
				boot:           client.Boot,
				link:           client.Link,
				sleep:          time.Sleep,
			}

			return runSetup(cmd.Context(), cfg, deps)
		},
	}
}

func runSetup(ctx context.Context, cfg setupConfig, deps setupDeps) error {
	output := cfg.output

	// Step 1: Register autostart.
	fmt.Fprint(output, "  [1/4] Registering autostart...")
	if err := deps.installService(); err != nil {
		fmt.Fprintln(output)

		return fmt.Errorf("register autostart: %w", err)
	}
	fmt.Fprintln(output, " done")

	// Step 2: Credential check + registration.
	if err := setupCredentials(ctx, cfg, deps, output); err != nil {
		return err
	}

	// Step 3: Start daemon + tray.
	fmt.Fprint(output, "  [3/4] Starting daemon...")
	if err := deps.startDaemon(); err != nil {
		fmt.Fprintln(output)

		return fmt.Errorf("start daemon: %w", err)
	}
	if cfg.trayPath != "" {
		if trayErr := deps.startTray(cfg.trayPath); trayErr != nil {
			fmt.Fprintf(output, " (tray: %v)", trayErr)
		}
	}
	fmt.Fprintln(output, " done")

	// Step 4: Wait for link code.
	return setupPollLink(ctx, cfg, deps, output)
}

func setupCredentials(
	ctx context.Context,
	cfg setupConfig,
	deps setupDeps,
	output io.Writer,
) error {
	fmt.Fprint(output, "  [2/4] Checking credentials...")

	if cfg.authToken != "" {
		verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := deps.verifyToken(verifyCtx, cfg.serverURL, cfg.authToken)
		cancel()

		if err == nil {
			fmt.Fprintln(output, " valid")

			return nil
		}

		fmt.Fprintln(output, " expired, re-registering")

		if removeErr := deps.removeFile(cfg.envPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("remove stale credentials: %w", removeErr)
		}
	} else {
		fmt.Fprintln(output, " none found, registering")
	}

	result, err := deps.register(ctx, cfg.serverURL, cfg.hostname)
	if err != nil {
		return fmt.Errorf(
			"could not reach savecraft.gg — check your internet connection: %w",
			err,
		)
	}

	if writeErr := deps.writeEnv(cfg.envPath, map[string]string{
		"SAVECRAFT_AUTH_TOKEN":  result.Token,
		"SAVECRAFT_SOURCE_UUID": result.SourceUUID,
		"SAVECRAFT_SERVER_URL":  cfg.serverURL,
	}); writeErr != nil {
		return fmt.Errorf("save credentials: %w", writeErr)
	}

	fmt.Fprintln(output, "  [2/4] Registered")

	return nil
}

func setupPollLink(
	ctx context.Context,
	cfg setupConfig,
	deps setupDeps,
	output io.Writer,
) error {
	fmt.Fprintln(output, "  [4/4] Waiting for daemon...")

	for _, delay := range setupPollSchedule() {
		deps.sleep(delay)

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("setup canceled: %w", err)
		}

		boot, bootErr := deps.boot(ctx)
		if bootErr != nil {
			continue // daemon not ready yet
		}

		if boot.State == localapi.StateError {
			return fmt.Errorf("daemon error: %s", boot.Error)
		}

		link, status, linkErr := deps.link(ctx)
		if linkErr != nil {
			continue
		}

		switch status {
		case http.StatusOK:
			if link.LinkCode != "" {
				fmt.Fprintln(output)
				fmt.Fprintln(output, "  =============================")
				fmt.Fprintf(output, "  Link code: %s\n", link.LinkCode)
				fmt.Fprintln(output, "  =============================")
				fmt.Fprintln(output)
				fmt.Fprintf(output, "  Visit %s to connect this device.\n", link.LinkURL)

				return nil
			}
		case http.StatusNotFound:
			fmt.Fprintln(output, "  Already linked to your account.")

			return nil
		}
	}

	// Final check after all retries.
	boot, bootErr := deps.boot(ctx)
	if bootErr != nil {
		return fmt.Errorf(
			"daemon not responding after 30s — check if port %s is in use by another program",
			cfg.statusPort,
		)
	}

	if boot.State == localapi.StateError {
		return fmt.Errorf("daemon error: %s", boot.Error)
	}

	return fmt.Errorf(
		"daemon started but link code not available after 30s — check logs: http://localhost:%s/logs",
		cfg.statusPort,
	)
}

// httpVerifyToken checks whether a source token is still valid by hitting
// the server's /api/v1/verify endpoint.
func httpVerifyToken(ctx context.Context, serverURL, token string) error {
	verifyURL := strings.TrimRight(serverURL, "/") + "/api/v1/verify"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, verifyURL, nil)
	if err != nil {
		return fmt.Errorf("create verify request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("verify request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token rejected (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// startTrayProcess launches the tray app as a background process.
func startTrayProcess(path string) error {
	trayCmd := exec.CommandContext(context.Background(), path)
	if err := trayCmd.Start(); err != nil {
		return fmt.Errorf("start tray: %w", err)
	}

	_ = trayCmd.Process.Release()

	return nil
}
