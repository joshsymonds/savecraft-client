//go:build !windows

package main

// acquireSingleton is a no-op on non-Windows platforms.
// Linux and macOS use service managers (systemd, launchd) that inherently
// prevent duplicate instances.
func acquireSingleton() (release func(), err error) {
	return func() {}, nil
}
