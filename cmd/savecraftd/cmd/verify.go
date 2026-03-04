package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
)

func buildVerifyCommand(appName string) *cobra.Command {
	var serverURL string

	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify that the stored auth token is valid",
		Long: `Check whether the daemon's auth token is accepted by the server.

Exits 0 if the token is valid, non-zero otherwise. Used by the installer
to decide whether to skip or re-run the source linking flow.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVerifyWithPath(cmd, serverURL, envfile.EnvFilePath(appName))
		},
	}

	verify.Flags().StringVar(&serverURL, "server", "", "server URL to verify against (required)")

	if err := verify.MarkFlagRequired("server"); err != nil {
		verify.Printf("warning: could not mark server flag required: %v\n", err)
	}

	return verify
}

func runVerifyWithPath(cmd *cobra.Command, serverURL string, envPath string) error {
	vars, err := envfile.Read(envPath)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	token := vars["SAVECRAFT_AUTH_TOKEN"]
	if token == "" {
		return fmt.Errorf("no auth token found in %s", envPath)
	}

	return verifyToken(cmd, serverURL, token)
}

func verifyToken(cmd *cobra.Command, serverURL string, token string) error {
	url := strings.TrimRight(serverURL, "/") + "/api/v1/verify"

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		cmd.Println("Token is valid")
		return nil
	}

	return fmt.Errorf("token invalid (server returned %d)", resp.StatusCode)
}
