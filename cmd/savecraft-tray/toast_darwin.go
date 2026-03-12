//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// showToast displays a macOS notification using osascript.
// macOS display notification doesn't support click-to-open-URL;
// the "Link Account" tray menu item serves as the fallback.
func showToast(title, body, _ string) error {
	script := fmt.Sprintf(
		`display notification %q with title %q`,
		body, title,
	)

	cmd := exec.Command("osascript", "-e", script)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("osascript toast: %w", err)
	}

	go func() { _ = cmd.Wait() }()

	return nil
}
