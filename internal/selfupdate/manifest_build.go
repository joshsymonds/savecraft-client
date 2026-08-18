package selfupdate

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/joshsymonds/savecraft-client/internal/daemon"
)

// checksumLineFields is "<sha256>  <file>" — sha256sum's two-column format.
const checksumLineFields = 2

// BuildManifest assembles the daemon update manifest for one release from a
// sha256sum-format checksum listing of the release's dist/ artifacts.
//
// Artifacts are named <appName>-daemon-<platform>[.exe] and
// <appName>-tray-<platform>[.exe]; the platform key is the GOOS-GOARCH pair a
// daemon computes for itself in Check, so the executable suffix is stripped
// from the key (never from the URL, which must name the real artifact).
// Signatures, installers and the checksum file itself are not entries.
func BuildManifest(
	version, installURL, appName, ed25519PublicKey string, checksums io.Reader,
) (*Manifest, error) {
	built := &Manifest{
		Version:          version,
		Ed25519PublicKey: ed25519PublicKey,
		Platforms:        map[string]daemon.UpdateInfo{},
		Tray:             map[string]daemon.UpdateInfo{},
	}
	daemonPrefix := appName + "-daemon-"
	trayPrefix := appName + "-tray-"

	scanner := bufio.NewScanner(checksums)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != checksumLineFields {
			continue
		}
		hash, fileName := fields[0], fields[1]
		if strings.HasSuffix(fileName, ".sig") || strings.HasSuffix(fileName, ".msi") ||
			strings.HasPrefix(fileName, "checksums-") {
			continue
		}
		info := daemon.UpdateInfo{
			URL:          fmt.Sprintf("%s/daemon/v%s/%s", installURL, version, fileName),
			SignatureURL: fmt.Sprintf("%s/daemon/v%s/%s.sig", installURL, version, fileName),
			SHA256:       hash,
		}
		switch {
		case strings.HasPrefix(fileName, daemonPrefix):
			built.Platforms[platformKey(strings.TrimPrefix(fileName, daemonPrefix))] = info
		case strings.HasPrefix(fileName, trayPrefix):
			built.Tray[platformKey(strings.TrimPrefix(fileName, trayPrefix))] = info
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	return built, nil
}

// platformKey turns an artifact's platform suffix into the manifest key:
// "windows-amd64.exe" → "windows-amd64", "linux-amd64" unchanged.
func platformKey(artifactPlatform string) string {
	return strings.TrimSuffix(artifactPlatform, ".exe")
}
