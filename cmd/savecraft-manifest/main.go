// Command savecraft-manifest builds the daemon update manifest for a release
// from the dist/ checksum listing (see selfupdate.BuildManifest).
//
//	savecraft-manifest -version 2.5.0 -install-url https://install.savecraft.gg \
//	    -app-name savecraft -checksums dist/checksums-sha256.txt \
//	    -public-key internal/signing/signing_key.pub -out dist/daemon-manifest.json
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshsymonds/savecraft-client/internal/selfupdate"
)

// ed25519SPKIPrefix is the DER SubjectPublicKeyInfo header for an Ed25519 key;
// the manifest publishes the raw signing key as base64(prefix || key).
var ed25519SPKIPrefix = []byte{ //nolint:gochecknoglobals // constant DER header
	0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("savecraft-manifest", flag.ContinueOnError)
	version := fs.String("version", "", "release version (manifest.version and the /daemon/v<version>/ path)")
	installURL := fs.String("install-url", "", "install origin, e.g. https://install.savecraft.gg")
	appName := fs.String("app-name", "", "artifact prefix, e.g. savecraft")
	checksums := fs.String("checksums", "", "sha256sum listing of dist/ artifacts")
	publicKey := fs.String("public-key", "", "raw 32-byte Ed25519 release public key")
	out := fs.String("out", "", "manifest path to write")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	for name, value := range map[string]string{
		"version": *version, "install-url": *installURL, "app-name": *appName,
		"checksums": *checksums, "public-key": *publicKey, "out": *out,
	} {
		if value == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}

	rawKey, err := os.ReadFile(filepath.Clean(*publicKey))
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	if len(rawKey) != ed25519.PublicKeySize {
		return fmt.Errorf("public key: got %d bytes, want %d", len(rawKey), ed25519.PublicKeySize)
	}
	der := append(append([]byte{}, ed25519SPKIPrefix...), rawKey...)

	sums, err := os.Open(filepath.Clean(*checksums))
	if err != nil {
		return fmt.Errorf("open checksums: %w", err)
	}
	defer func() { _ = sums.Close() }()

	built, err := selfupdate.BuildManifest(
		*version, *installURL, *appName, base64.StdEncoding.EncodeToString(der), sums,
	)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}
	if len(built.Platforms) == 0 {
		return errors.New("no daemon artifacts found in checksums")
	}
	data, err := json.MarshalIndent(built, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
