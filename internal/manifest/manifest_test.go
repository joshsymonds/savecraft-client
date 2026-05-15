package manifest

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

type sampleManifest struct {
	Version  string            `json:"version"`
	SHA256   string            `json:"sha256"`
	Platform map[string]string `json:"platform"`
}

// panicOnParse fails the test if json.Unmarshal is ever invoked on it. Used to
// prove VerifyAndParse never parses when signature verification fails.
type panicOnParse struct{}

func (panicOnParse) UnmarshalJSON([]byte) error {
	panic("UnmarshalJSON called before signature verification passed")
}

func mustKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := signing.GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

func TestVerifyAndParse_ValidSignature(t *testing.T) {
	pub, priv := mustKeypair(t)

	// Literal bytes as they would be served — note deliberate indentation and
	// key ordering. The signature is over exactly these bytes.
	raw := []byte(`{
  "version": "1.4.2",
  "sha256": "abc123",
  "platform": { "linux-amd64": "url-here" }
}`)
	sig := signing.Sign(priv, raw)

	got, err := VerifyAndParse[sampleManifest](pub, raw, sig)
	if err != nil {
		t.Fatalf("VerifyAndParse: unexpected error: %v", err)
	}
	if got.Version != "1.4.2" || got.SHA256 != "abc123" {
		t.Fatalf("parsed struct = %+v, want version=1.4.2 sha256=abc123", got)
	}
	if got.Platform["linux-amd64"] != "url-here" {
		t.Fatalf("platform map = %v, want linux-amd64=url-here", got.Platform)
	}
}

func TestVerifyAndParse_TamperedByteRejectedBeforeParse(t *testing.T) {
	pub, priv := mustKeypair(t)

	raw := []byte(`{"version":"1.0.0","sha256":"deadbeef","platform":{}}`)
	sig := signing.Sign(priv, raw)

	// Flip one byte after signing.
	tampered := make([]byte, len(raw))
	copy(tampered, raw)
	tampered[10] ^= 0x01

	// panicOnParse panics if UnmarshalJSON is reached; a passing test proves
	// verification strictly precedes parsing.
	got, err := VerifyAndParse[panicOnParse](pub, tampered, sig)
	if err == nil {
		t.Fatal("expected error for tampered manifest bytes, got nil")
	}
	if got != (panicOnParse{}) {
		t.Fatalf("expected zero value on failure, got %+v", got)
	}
}

func TestVerifyAndParse_ZeroValueOnFailure(t *testing.T) {
	pub, priv := mustKeypair(t)
	raw := []byte(`{"version":"9.9.9","sha256":"x","platform":{"p":"q"}}`)
	sig := signing.Sign(priv, raw)
	tampered := append([]byte(nil), raw...)
	tampered[2] ^= 0xFF

	got, err := VerifyAndParse[sampleManifest](pub, tampered, sig)
	if err == nil {
		t.Fatal("expected verification error")
	}
	if got.Version != "" || got.SHA256 != "" || got.Platform != nil {
		t.Fatalf("expected zero-value struct on failure, got %+v", got)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv := mustKeypair(t)
	wrongPub, _ := mustKeypair(t)

	raw := []byte(`{"version":"1.0.0"}`)
	sig := signing.Sign(priv, raw)

	if err := Verify(wrongPub, raw, sig); err == nil {
		t.Fatal("expected error verifying with wrong public key")
	}
}

func TestVerify_BadSignatureLength(t *testing.T) {
	pub, _ := mustKeypair(t)
	if err := Verify(pub, []byte(`{}`), []byte("too-short")); err == nil {
		t.Fatal("expected error for bad signature length")
	}
}

func TestVerify_ShortPublicKey(t *testing.T) {
	if err := Verify([]byte("bad"), []byte(`{}`), make([]byte, ed25519.SignatureSize)); err == nil {
		t.Fatal("expected error for short public key")
	}
}

func TestVerify_EmptyInputs(t *testing.T) {
	pub, priv := mustKeypair(t)

	// Empty manifest bytes, real signature over non-empty content: must error,
	// must not panic.
	sig := signing.Sign(priv, []byte("something"))
	if err := Verify(pub, []byte{}, sig); err == nil {
		t.Fatal("expected error verifying empty manifest bytes against mismatched signature")
	}

	// Empty signature: bad length, must error not panic.
	if err := Verify(pub, []byte(`{}`), []byte{}); err == nil {
		t.Fatal("expected error for empty signature")
	}
}

// TestVerifyAndParse_VerifiesLiteralBytesNotCanonicalForm proves the helper
// verifies the exact bytes received, never a re-serialized/canonical form. A
// signature over compact JSON must NOT validate a semantically-identical but
// differently-formatted document.
func TestVerifyAndParse_VerifiesLiteralBytesNotCanonicalForm(t *testing.T) {
	pub, priv := mustKeypair(t)

	compact := []byte(`{"version":"2.0.0","sha256":"hh","platform":{"a":"b"}}`)
	sig := signing.Sign(priv, compact)

	// Same logical content, different whitespace + key order.
	pretty := []byte(`{
  "platform": { "a": "b" },
  "sha256": "hh",
  "version": "2.0.0"
}`)

	// Sanity: both are valid JSON encoding the same data.
	var a, b sampleManifest
	if err := json.Unmarshal(compact, &a); err != nil {
		t.Fatalf("compact not valid JSON: %v", err)
	}
	if err := json.Unmarshal(pretty, &b); err != nil {
		t.Fatalf("pretty not valid JSON: %v", err)
	}

	// The signature over `compact` must reject `pretty`.
	if _, err := VerifyAndParse[sampleManifest](pub, pretty, sig); err == nil {
		t.Fatal("signature over compact bytes must NOT verify a reformatted document")
	}

	// And it must accept the exact signed bytes.
	got, err := VerifyAndParse[sampleManifest](pub, compact, sig)
	if err != nil {
		t.Fatalf("exact signed bytes failed to verify: %v", err)
	}
	if got.Version != "2.0.0" {
		t.Fatalf("version = %q, want 2.0.0", got.Version)
	}
}

func TestVerifyAndParse_InvalidJSONAfterValidSignature(t *testing.T) {
	pub, priv := mustKeypair(t)

	// Validly signed, but not valid JSON for the target type.
	raw := []byte(`this is not json`)
	sig := signing.Sign(priv, raw)

	_, err := VerifyAndParse[sampleManifest](pub, raw, sig)
	if err == nil {
		t.Fatal("expected JSON decode error after successful verification")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "json") {
		t.Errorf("error = %q, want it to indicate a parse/json failure", err)
	}
}
