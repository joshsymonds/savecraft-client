package signing

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	data := []byte("hello wasm plugin")
	sig := Sign(priv, data)

	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("verify round-trip: %v", err)
	}
}

func TestVerify_TamperedData(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	data := []byte("original data")
	sig := Sign(priv, data)

	tampered := []byte("tampered data")
	if err := Verify(pub, tampered, sig); err == nil {
		t.Fatal("expected error for tampered data")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	wrongPub, _, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate second keypair: %v", err)
	}

	data := []byte("signed with different key")
	sig := Sign(priv, data)

	if err := Verify(wrongPub, data, sig); err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestVerify_InvalidSignatureSize(t *testing.T) {
	pub, _, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	err = Verify(pub, []byte("data"), []byte("short"))
	if err == nil {
		t.Fatal("expected error for invalid signature size")
	}
	if !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("error = %q, want to contain 'invalid signature'", err)
	}
}

func TestVerify_InvalidPublicKeySize(t *testing.T) {
	err := Verify([]byte("bad"), []byte("data"), make([]byte, ed25519.SignatureSize))
	if err == nil {
		t.Fatal("expected error for invalid public key size")
	}
	if !strings.Contains(err.Error(), "invalid public key") {
		t.Errorf("error = %q, want to contain 'invalid public key'", err)
	}
}

func TestGenerateKeypair_KeySizes(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
}
