// Package signing provides Ed25519 sign/verify operations for WASM plugins.
package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
)

// GenerateKeypair creates a new Ed25519 keypair.
func GenerateKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return pub, priv, nil
}

// Sign produces an Ed25519 signature of data using the given private key.
func Sign(privateKey ed25519.PrivateKey, data []byte) []byte {
	return ed25519.Sign(privateKey, data)
}

// Verify checks that signature is a valid Ed25519 signature of data by publicKey.
func Verify(publicKey ed25519.PublicKey, data, signature []byte) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key: got %d bytes, want %d", len(publicKey), ed25519.PublicKeySize)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature: got %d bytes, want %d", len(signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(publicKey, data, signature) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}
