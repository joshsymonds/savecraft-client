// Package appname provides shared helpers for deriving platform-specific
// names from the daemon's compile-time application name.
package appname

import "strings"

// BinaryName returns the daemon binary name derived from appName
// (e.g. "savecraft" → "savecraft-daemon").
func BinaryName(appName string) string {
	return appName + "-daemon"
}

// TitleName returns appName with the first letter capitalized,
// matching platform conventions for macOS and Windows directory names.
func TitleName(appName string) string {
	if appName == "" {
		return appName
	}
	return strings.ToUpper(appName[:1]) + appName[1:]
}
