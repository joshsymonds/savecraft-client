package cmd

import "github.com/spf13/cobra"

func buildVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the daemon version",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("savecraft-daemon %s\n", version)
		},
	}
}
