package signing

import (
	"crypto/ed25519"
	_ "embed"
)

//go:embed signing_key.pub
var embeddedPublicKey []byte

// PublicKey returns the embedded Ed25519 public key used to verify plugin signatures.
func PublicKey() ed25519.PublicKey {
	return ed25519.PublicKey(embeddedPublicKey)
}
