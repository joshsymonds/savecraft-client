// Package envfile reads and writes KEY=VALUE environment files
// used by the savecraft daemon for configuration persistence.
package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Read parses a KEY=VALUE file into a map. Comments (lines starting with #)
// and blank lines are skipped. Returns an empty map if the file does not exist.
func Read(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return make(map[string]string), nil
		}

		return nil, fmt.Errorf("open env file: %w", err)
	}
	defer file.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		vars[key] = value
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("read env file: %w", scanErr)
	}

	return vars, nil
}

// Write persists key=value pairs to the given path, creating parent
// directories as needed. The file is written with 0600 permissions
// since it may contain authentication tokens.
func Write(path string, vars map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	var builder strings.Builder

	builder.WriteString("# Savecraft daemon configuration\n")
	builder.WriteString("# Written by savecraftd pair\n\n")

	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(vars[key])
		builder.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	return nil
}

// ConfigDir returns the platform-appropriate configuration directory for savecraft.
//
//   - Linux: $XDG_CONFIG_HOME/savecraft or ~/.config/savecraft
//   - macOS: ~/Library/Application Support/Savecraft
//   - Windows: %APPDATA%\Savecraft
func ConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		return darwinConfigDir()
	case "windows":
		return windowsConfigDir()
	default:
		return linuxConfigDir()
	}
}

// EnvFilePath returns the full path to the daemon's env file.
func EnvFilePath() string {
	return filepath.Join(ConfigDir(), "env")
}

func darwinConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, "Library", "Application Support", "Savecraft")
}

func windowsConfigDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "Savecraft")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, "AppData", "Roaming", "Savecraft")
}

func linuxConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "savecraft")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "savecraft")
	}

	return filepath.Join(home, ".config", "savecraft")
}
