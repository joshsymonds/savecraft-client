// Command savecraft-sign signs a file with Ed25519, writing a .sig alongside it.
// Usage: savecraft-sign <file> [private-key-path]
package main

import (
	"crypto/ed25519"
	"fmt"
	"os"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: savecraft-sign <file> [private-key-path]\n")
		os.Exit(1)
	}

	filePath := os.Args[1]
	keyPath := "internal/signing/signing_key.priv"
	if len(os.Args) > 2 {
		keyPath = os.Args[2]
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read private key: %v\n", err)
		os.Exit(1)
	}

	if len(keyBytes) != ed25519.PrivateKeySize {
		fmt.Fprintf(os.Stderr, "invalid private key: got %d bytes, want %d\n", len(keyBytes), ed25519.PrivateKeySize)
		os.Exit(1)
	}

	sig := signing.Sign(ed25519.PrivateKey(keyBytes), data)

	sigPath := filePath + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write signature: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("signed %s → %s (%d bytes)\n", filePath, sigPath, len(sig))
}
