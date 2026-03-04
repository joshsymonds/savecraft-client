package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func buildVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the daemon version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("savecraft-daemon %s\n", version)
		},
	}
}
