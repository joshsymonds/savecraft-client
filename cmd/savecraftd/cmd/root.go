// Package cmd implements the cobra CLI for the savecraft daemon.
package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
	"github.com/joshsymonds/savecraft.gg/internal/svcmgr"
)

// Execute builds the command tree and runs the root command.
// When invoked with no subcommand, the daemon runs (same as "run").
// All string params are compile-time defaults (set via ldflags in main).
func Execute(version, serverURL, installURL, appName, statusPort, frontendURL string) error {
	runFn := buildRunFunc(serverURL, installURL, appName, statusPort, frontendURL)

	root := &cobra.Command{
		Use:          "savecraftd",
		Short:        "Savecraft daemon — syncs game saves to the cloud",
		SilenceUsage: true,
		RunE:         runFn,
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon (default when no subcommand is given)",
		RunE:  runFn,
	}

	svcCfg := svcmgr.Config{
		Name:        appName + "-daemon",
		DisplayName: "Savecraft Daemon",
		Description: "Syncs game saves to the cloud via Savecraft",
		AppName:     appName,
	}

	root.AddCommand(runCmd)
	root.AddCommand(buildServiceCommand("install", "Install the daemon as an OS service", svcCfg))
	root.AddCommand(buildUninstallCommand(svcCfg, appName))
	root.AddCommand(buildServiceCommand("start", "Start the daemon OS service", svcCfg))
	root.AddCommand(buildStopCommand(statusPort))
	root.AddCommand(buildRepairCommand(statusPort))
	root.AddCommand(buildUpdatePluginsCommand(statusPort))
	root.AddCommand(buildSetupCommand(serverURL, appName, statusPort, frontendURL))
	root.AddCommand(buildVerifyCommand(appName, serverURL))
	root.AddCommand(buildVersionCommand(version))

	if err := root.Execute(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

// buildServiceCommand creates a cobra command that invokes svcmgr.Control
// for the given action (install, uninstall, start).
func buildServiceCommand(action, short string, cfg svcmgr.Config) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := svcmgr.Control(cfg, action); err != nil {
				return fmt.Errorf("service %s: %w", action, err)
			}

			fmt.Printf("Service %s: success\n", action)

			return nil
		},
	}
}

// buildRepairCommand creates a cobra command that triggers re-pairing via the
// local API /repair endpoint.
func buildRepairCommand(statusPort string) *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Re-pair the daemon with a different user account",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := localapi.NewClient("http://localhost:" + statusPort)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			resp, err := client.Repair(ctx)
			if err != nil {
				return fmt.Errorf("repair: %w", err)
			}

			fmt.Printf("Re-pairing initiated.\n\n  Link this source: %s\n  Code: %s\n\n", resp.LinkURL, resp.LinkCode)

			return nil
		},
	}
}

// buildUpdatePluginsCommand creates a cobra command that triggers an immediate
// plugin update check via the local API /update-plugins endpoint.
func buildUpdatePluginsCommand(statusPort string) *cobra.Command {
	return &cobra.Command{
		Use:   "update-plugins",
		Short: "Check for and download plugin updates",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := localapi.NewClient("http://localhost:" + statusPort)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			resp, err := client.UpdatePlugins(ctx)
			if err != nil {
				return fmt.Errorf("update plugins: %w", err)
			}

			if len(resp.Updated) == 0 {
				fmt.Println("All plugins are up to date.")
			} else {
				fmt.Printf("Updated: %s\n", strings.Join(resp.Updated, ", "))
			}

			return nil
		},
	}
}

// buildStopCommand creates a cobra command that stops the daemon via the
// local API /shutdown endpoint (cross-platform, no platform-specific stop).
func buildStopCommand(statusPort string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon via local API",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := localapi.NewClient("http://localhost:" + statusPort)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := client.Shutdown(ctx); err != nil {
				return fmt.Errorf("stop daemon: %w", err)
			}

			fmt.Println("Daemon stopped")

			return nil
		},
	}
}
