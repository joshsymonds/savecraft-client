package selfupdate

import (
	"context"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/signing"
)

const releaseChecksums = `aaa1  savecraft-daemon-linux-amd64
bbb2  savecraft-daemon-windows-amd64.exe
ccc3  savecraft-tray-windows-amd64.exe
ddd4  savecraft-daemon-windows-amd64.exe.sig
eee5  savecraft.msi
fff6  checksums-sha256.txt
`

// The daemon looks its platform up as runtime.GOOS-runtime.GOARCH
// ("windows-amd64"); the release artifact is named with the .exe suffix.
// Keying the manifest by the artifact name (the historical "windows-amd64.exe")
// meant no Windows daemon ever found itself in the manifest, so none ever
// self-updated: 70 sources still on pre-July builds on 2026-08-17.
func TestBuildManifest_KeysWindowsBinariesByGOOSGOARCH(t *testing.T) {
	m, err := BuildManifest("2.5.0", "https://install.savecraft.gg", "savecraft", "pubkey",
		strings.NewReader(releaseChecksums))
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if m.Version != "2.5.0" || m.Ed25519PublicKey != "pubkey" {
		t.Fatalf("version/pubkey = %q/%q", m.Version, m.Ed25519PublicKey)
	}

	win, ok := m.Platforms["windows-amd64"]
	if !ok {
		t.Fatalf("platforms keys = %v, want windows-amd64", keys(m.Platforms))
	}
	if win.URL != "https://install.savecraft.gg/daemon/v2.5.0/savecraft-daemon-windows-amd64.exe" ||
		win.SignatureURL != "https://install.savecraft.gg/daemon/v2.5.0/savecraft-daemon-windows-amd64.exe.sig" ||
		win.SHA256 != "bbb2" {
		t.Fatalf("windows-amd64 entry = %+v", win)
	}
	if got := m.Platforms["linux-amd64"].SHA256; got != "aaa1" {
		t.Fatalf("linux-amd64 sha = %q", got)
	}
	if _, has := m.Platforms["windows-amd64.exe"]; has {
		t.Fatal("artifact-named key windows-amd64.exe must not exist")
	}
	if len(m.Platforms) != 2 {
		t.Fatalf("platforms = %v, want exactly linux-amd64 and windows-amd64 (.sig/.msi/checksums skipped)",
			keys(m.Platforms))
	}

	tray, ok := m.Tray["windows-amd64"]
	if !ok || tray.SHA256 != "ccc3" ||
		tray.URL != "https://install.savecraft.gg/daemon/v2.5.0/savecraft-tray-windows-amd64.exe" {
		t.Fatalf("tray = %+v", m.Tray)
	}
}

// End to end: a manifest built from release checksums, signed and served, is
// found by a Windows daemon's own Check — with the tray alongside it.
func TestCheck_FindsWindowsDaemonInBuiltManifest(t *testing.T) {
	m, err := BuildManifest("2.5.0", "https://install.savecraft.gg", "savecraft", "pubkey",
		strings.NewReader(releaseChecksums))
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	pub, priv, _ := signing.GenerateKeypair()
	srv, _ := signedManifestServer(t, priv, *m)
	u := newTestUpdater(t, srv, pub)

	result, err := u.Check(context.Background(), "2.2.0", "windows-amd64")
	if err != nil {
		t.Fatalf("Check(windows-amd64): %v", err)
	}
	if result.Daemon.URL != "https://install.savecraft.gg/daemon/v2.5.0/savecraft-daemon-windows-amd64.exe" {
		t.Fatalf("daemon URL = %q", result.Daemon.URL)
	}
	if result.Tray == nil || result.Tray.SHA256 != "ccc3" {
		t.Fatalf("tray = %+v, want the windows tray artifact", result.Tray)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
