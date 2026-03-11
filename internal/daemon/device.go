package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

const dmiVendorPath = "/sys/devices/virtual/dmi/id/board_vendor"

// DetectDevice reads DMI data to identify special hardware.
// Returns "steam_deck" on Valve hardware, empty string otherwise.
func DetectDevice() string {
	return detectDeviceFrom(dmiVendorPath)
}

func detectDeviceFrom(path string) string {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	vendor := strings.TrimSpace(string(data))
	if vendor == "Valve" {
		return "steam_deck"
	}
	return ""
}
