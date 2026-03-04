package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/appname"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/pairclient"
)

func buildPairCommand(appName, frontendURL string) *cobra.Command {
	var force bool

	var serverURL string

	binName := appname.BinaryName(appName)

	pair := &cobra.Command{
		Use:   "pair",
		Short: "Pair this device with your Savecraft account",
		Long: fmt.Sprintf(`Pair this device by entering a 6-digit code from %s/devices.

The code is exchanged for an API token which is written to the daemon's
configuration file. Run '%s' (or restart the service) afterward
to start syncing saves.`, frontendURL, binName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPairWithPath(cmd, serverURL, force, envfile.EnvFilePath(appName), binName)
		},
	}

	pair.Flags().StringVar(&serverURL, "server", "", "server URL to pair with (required)")
	pair.Flags().BoolVar(&force, "force", false, "overwrite existing credentials")

	if err := pair.MarkFlagRequired("server"); err != nil {
		pair.Printf("warning: could not mark server flag required: %v\n", err)
	}

	return pair
}

func runPairWithPath(cmd *cobra.Command, serverURL string, force bool, envPath, binaryName string) error {
	// Check for existing credentials.
	existing, err := envfile.Read(envPath)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	if existing["SAVECRAFT_AUTH_TOKEN"] != "" && !force {
		cmd.PrintErrln("This device is already paired.")
		cmd.PrintErrln("Use --force to overwrite existing credentials.")

		return fmt.Errorf("already paired (use --force to overwrite)")
	}

	// Prompt for the pairing code.
	reader := resolveInputReader(cmd)
	defer reader.Close()

	code, err := promptForCodeFromReader(cmd, reader)
	if err != nil {
		return fmt.Errorf("read pairing code: %w", err)
	}

	// Exchange code for token.
	cmd.Println("Pairing...")

	result, err := pairclient.ClaimCode(serverURL, code)
	if err != nil {
		return fmt.Errorf("pair failed: %w", err)
	}

	// Write credentials to env file.
	if writeErr := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_AUTH_TOKEN": result.Token,
		"SAVECRAFT_SERVER_URL": result.ServerURL,
	}); writeErr != nil {
		return fmt.Errorf("write config: %w", writeErr)
	}

	cmd.Println("Paired successfully!")
	cmd.Printf("Config written to %s\n", envPath)
	cmd.Println("")
	cmd.Println("Start the daemon with:")
	cmd.Printf("  %s\n", binaryName)

	return nil
}

var codePattern = regexp.MustCompile(`^\d{6}$`)

func promptForCodeFromReader(cmd *cobra.Command, reader io.Reader) (string, error) {
	cmd.Print("Enter 6-digit pairing code: ")

	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}

		return "", fmt.Errorf("no input received")
	}

	code := strings.ReplaceAll(strings.TrimSpace(scanner.Text()), " ", "")
	if !codePattern.MatchString(code) {
		return "", fmt.Errorf("invalid code %q — expected 6 digits", code)
	}

	return code, nil
}

// resolveInputReader returns the appropriate reader for interactive input.
// If cobra has a buffer set (tests), use that. If stdin is a pipe (curl|bash),
// open /dev/tty for interactive input. The caller must Close the returned reader.
func resolveInputReader(cmd *cobra.Command) io.ReadCloser {
	// If a custom reader was set via cmd.SetIn (e.g., in tests), use it.
	// We detect this by checking if InOrStdin returns something other than os.Stdin.
	cmdIn := cmd.InOrStdin()
	if cmdIn != os.Stdin {
		return io.NopCloser(cmdIn)
	}

	// Check if stdin is a pipe.
	stdinInfo, err := os.Stdin.Stat()
	if err == nil && (stdinInfo.Mode()&os.ModeCharDevice) == 0 {
		// stdin is a pipe — try /dev/tty for interactive input.
		tty, ttyErr := os.Open("/dev/tty")
		if ttyErr == nil {
			return tty
		}
	}

	return io.NopCloser(os.Stdin)
}
