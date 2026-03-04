// Package cmd implements the cobra CLI for the savecraft daemon.
package cmd

import (
	"fmt"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"

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
	}

	root.AddCommand(runCmd)
	root.AddCommand(buildServiceCommand("install", "Install the daemon as an OS service", svcCfg))
	root.AddCommand(buildServiceCommand("uninstall", "Remove the daemon OS service", svcCfg))
	root.AddCommand(buildServiceCommand("start", "Start the daemon OS service", svcCfg))
	root.AddCommand(buildServiceCommand("stop", "Stop the daemon OS service", svcCfg))
	root.AddCommand(buildVerifyCommand(appName))
	root.AddCommand(buildVersionCommand(version))

	if err := root.Execute(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

// buildServiceCommand creates a cobra command that invokes service.Control
// for the given action (install, uninstall, start, stop).
func buildServiceCommand(action, short string, cfg svcmgr.Config) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			prog := svcmgr.New(cfg, nil)

			svc, svcErr := service.New(prog, prog.ServiceConfig())
			if svcErr != nil {
				return fmt.Errorf("create service: %w", svcErr)
			}

			if err := service.Control(svc, action); err != nil {
				return fmt.Errorf("service %s: %w", action, err)
			}

			fmt.Printf("Service %s: success\n", action)

			return nil
		},
	}
}
