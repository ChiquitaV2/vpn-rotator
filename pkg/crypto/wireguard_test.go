package crypto

import (
	"encoding/base64"
	"testing"
)

// TestGenerateKeyPairAndDerive verifies generated keys are valid, decode to 32 bytes,
// and that DerivePublicKey(private) equals the generated public key.
func TestGenerateKeyPairAndDerive(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}
	if !IsValidWireGuardKey(kp.PrivateKey) {
		t.Fatalf("generated private key is invalid")
	}
	if !IsValidWireGuardKey(kp.PublicKey) {
		t.Fatalf("generated public key is invalid")
	}

	derivedPub, err := DerivePublicKey(kp.PrivateKey)
	if err != nil {
		t.Fatalf("DerivePublicKey error: %v", err)
	}
	if derivedPub != kp.PublicKey {
		t.Fatalf("derived public key does not match generated public key")
	}

	privBytes, err := base64.StdEncoding.DecodeString(kp.PrivateKey)
	if err != nil || len(privBytes) != 32 {
		t.Fatalf("private key decode length unexpected: %v, len=%d", err, len(privBytes))
	}
	pubBytes, err := base64.StdEncoding.DecodeString(kp.PublicKey)
	if err != nil || len(pubBytes) != 32 {
		t.Fatalf("public key decode length unexpected: %v, len=%d", err, len(pubBytes))
	}
}

// TestDerivePublicKey_Errors checks invalid base64 and incorrect-length inputs produce errors.
func TestDerivePublicKey_Errors(t *testing.T) {
	// invalid base64
	if _, err := DerivePublicKey("not-base64!!"); err == nil {
		t.Fatalf("expected error for invalid base64 input")
	}

	// incorrect length (31 bytes -> base64 not 44 chars for 32 bytes)
	b := make([]byte, 31)
	for i := range b {
		b[i] = byte(i)
	}
	shortBase64 := base64.StdEncoding.EncodeToString(b)
	if _, err := DerivePublicKey(shortBase64); err == nil {
		t.Fatalf("expected error for private key with incorrect length")
	}
}

// TestIsValidWireGuardKey_Cases verifies various valid and invalid inputs for IsValidWireGuardKey.
func TestIsValidWireGuardKey_Cases(t *testing.T) {
	// too short
	if IsValidWireGuardKey("short") {
		t.Fatalf("expected 'short' to be invalid")
	}

	// invalid characters but correct length (44)
	invalidChars := ""
	for i := 0; i < 44; i++ {
		invalidChars += "!"
	}
	if IsValidWireGuardKey(invalidChars) {
		t.Fatalf("expected string with invalid chars to be invalid")
	}

	// valid generated key
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}
	if !IsValidWireGuardKey(kp.PrivateKey) {
		t.Fatalf("expected generated private key to be valid")
	}
}
