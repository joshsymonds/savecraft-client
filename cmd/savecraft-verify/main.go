// Command savecraft-verify verifies a file's Ed25519 signature and prints its SHA-256 hash.
// Usage: savecraft-verify <file> [public-key-path]
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: savecraft-verify <file> [public-key-path]\n")
		os.Exit(1)
	}

	filePath := os.Args[1]
	sigPath := filePath + ".sig"

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	sigBytes, err := os.ReadFile(sigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read signature: %v\n", err)
		os.Exit(1)
	}

	var pubKey ed25519.PublicKey
	if len(os.Args) > 2 {
		keyBytes, readErr := os.ReadFile(os.Args[2])
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read public key: %v\n", readErr)
			os.Exit(1)
		}
		if len(keyBytes) != ed25519.PublicKeySize {
			fmt.Fprintf(os.Stderr, "invalid public key: got %d bytes, want %d\n", len(keyBytes), ed25519.PublicKeySize)
			os.Exit(1)
		}
		pubKey = ed25519.PublicKey(keyBytes)
	} else {
		pubKey = signing.PublicKey()
	}

	if err := signing.Verify(pubKey, data, sigBytes); err != nil {
		fmt.Fprintf(os.Stderr, "verification failed: %v\n", err)
		os.Exit(1)
	}

	hash := sha256.Sum256(data)
	fmt.Printf("OK %x %s\n", hash, filePath)
}
