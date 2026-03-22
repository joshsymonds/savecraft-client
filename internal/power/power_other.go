//go:build !windows

// Package power provides platform-specific power event monitoring.
// On non-Windows platforms, this is a no-op — service managers (systemd, launchd)
// handle daemon lifecycle across sleep/wake.
package power

import "context"

// Monitor starts listening for power events. On non-Windows platforms, this
// returns a nil channel (never signals) and a no-op stop function.
func Monitor(_ context.Context) (<-chan struct{}, func()) {
	return nil, func() {}
}
