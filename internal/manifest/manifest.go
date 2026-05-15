// Package manifest provides verify-then-parse handling of signed release
// manifests. The signature is always over the literal serialized bytes of the
// manifest as served; verification strictly precedes any JSON decoding so a
// caller can never act on an unauthenticated field.
package manifest

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

// Verify checks that sigBytes is a valid Ed25519 signature by pub over the
// exact manifestBytes. It performs no parsing or canonicalization: the bytes
// passed in must be the literal bytes that were signed and served.
func Verify(pub ed25519.PublicKey, manifestBytes, sigBytes []byte) error {
	if err := signing.Verify(pub, manifestBytes, sigBytes); err != nil {
		return fmt.Errorf("verify manifest signature: %w", err)
	}
	return nil
}

// VerifyAndParse verifies the detached signature over the literal manifestBytes
// and, only if verification succeeds, decodes them into T. On any verification
// failure it returns the zero value of T and never attempts to parse.
func VerifyAndParse[T any](pub ed25519.PublicKey, manifestBytes, sigBytes []byte) (T, error) {
	var zero T
	if err := Verify(pub, manifestBytes, sigBytes); err != nil {
		return zero, err
	}
	var parsed T
	if err := json.Unmarshal(manifestBytes, &parsed); err != nil {
		return zero, fmt.Errorf("parse verified manifest: %w", err)
	}
	return parsed, nil
}
