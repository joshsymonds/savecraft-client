// Package cmd implements the cobra CLI for the savecraft daemon.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Execute builds the command tree and runs the root command.
// When invoked with no subcommand, the daemon runs (same as "run").
// serverURL and installURL are compile-time defaults (set via ldflags in main).
func Execute(version, serverURL, installURL string) error {
	runFn := buildRunFunc(serverURL, installURL)

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

	root.AddCommand(runCmd)
	root.AddCommand(buildPairCommand())
	root.AddCommand(buildVersionCommand(version))

	if err := root.Execute(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}
