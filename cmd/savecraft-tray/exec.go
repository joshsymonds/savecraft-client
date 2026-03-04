package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runCommand executes a command and returns an error if it fails.
func runCommand(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}

	return nil
}

// runCommandWithStdin pipes text into a command's stdin.
func runCommandWithStdin(name, input string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdin = strings.NewReader(input)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}
