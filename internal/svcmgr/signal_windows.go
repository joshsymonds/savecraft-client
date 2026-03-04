//go:build windows

package svcmgr

import "os"

func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
