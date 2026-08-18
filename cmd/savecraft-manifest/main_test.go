package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/selfupdate"
)

func TestRun_WritesManifestWithDERPublicKey(t *testing.T) {
	dir := t.TempDir()
	checksums := filepath.Join(dir, "checksums-sha256.txt")
	if err := os.WriteFile(checksums, []byte("bbb2  savecraft-daemon-windows-amd64.exe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rawKey := make([]byte, 32)
	for i := range rawKey {
		rawKey[i] = byte(i)
	}
	keyPath := filepath.Join(dir, "signing_key.pub")
	if err := os.WriteFile(keyPath, rawKey, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "daemon-manifest.json")

	err := run([]string{
		"-version", "2.5.0", "-install-url", "https://install.savecraft.gg", "-app-name", "savecraft",
		"-checksums", checksums, "-public-key", keyPath, "-out", out,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var m selfupdate.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != "2.5.0" || m.Platforms["windows-amd64"].SHA256 != "bbb2" {
		t.Fatalf("manifest = %+v", m)
	}
	// The manifest publishes the key as base64 SubjectPublicKeyInfo DER, as
	// the workflow always has: 12-byte Ed25519 prefix + raw key.
	der, err := base64.StdEncoding.DecodeString(m.Ed25519PublicKey)
	if err != nil {
		t.Fatalf("pubkey not base64: %v", err)
	}
	wantPrefix := []byte{0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00}
	if len(der) != 44 || string(der[:12]) != string(wantPrefix) || string(der[12:]) != string(rawKey) {
		t.Fatalf("pubkey DER = %x", der)
	}
}
