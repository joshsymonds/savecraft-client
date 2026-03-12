//go:build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
)

// showToast displays a desktop notification using notify-send.
// If notify-send is not installed, returns an error (non-fatal to caller).
func showToast(title, body, _ string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return fmt.Errorf("notify-send not found: %w", err)
	}

	// --app-name for notification grouping.
	// notify-send doesn't support click-to-open-URL natively;
	// the "Link Account" tray menu item serves as the fallback.
	cmd := exec.CommandContext(context.Background(), path, "--app-name=Savecraft", title, body)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("notify-send: %w", err)
	}

	go func() { _ = cmd.Wait() }()

	return nil
}
