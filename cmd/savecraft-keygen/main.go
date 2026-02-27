// Command savecraft-keygen generates an Ed25519 keypair for plugin signing.
// The public key is written to internal/signing/signing_key.pub (checked in)
// and the private key to internal/signing/signing_key.priv (gitignored).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

func main() {
	dir := "internal/signing"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	pubPath := filepath.Join(dir, "signing_key.pub")
	if err := os.WriteFile(pubPath, pub, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write public key: %v\n", err)
		os.Exit(1)
	}

	privPath := filepath.Join(dir, "signing_key.priv")
	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write private key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("public key:  %s (%d bytes)\n", pubPath, len(pub))
	fmt.Printf("private key: %s (%d bytes)\n", privPath, len(priv))
}
