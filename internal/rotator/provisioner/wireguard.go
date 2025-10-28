package provisioner

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// KeyPair represents a WireGuard key pair (private and public keys).
type KeyPair struct {
	PrivateKey string // Base64 encoded private key
	PublicKey  string // Base64 encoded public key
}

// GenerateKeyPair generates a new WireGuard key pair using Curve25519.
// The private key is "clamped" according to WireGuard specification:
// - Clear lowest 3 bits (makes it a multiple of 8)
// - Clear highest bit (ensures it's in the prime-order subgroup)
// - Set second-highest bit (ensures non-zero)
func GenerateKeyPair() (*KeyPair, error) {
	// Step 1: Generate 32 random bytes for private key
	privateKey := make([]byte, 32)
	if _, err := rand.Read(privateKey); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Step 2: "Clamp" the key per WireGuard specification
	// This ensures the key is in the correct mathematical group
	privateKey[0] &= 248  // Clear lowest 3 bits: 11111000 = 248
	privateKey[31] &= 127 // Clear highest bit: 01111111 = 127
	privateKey[31] |= 64  // Set second-highest bit: 01000000 = 64

	// Step 3: Generate public key using Curve25519 scalar multiplication
	publicKey, err := curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to generate public key: %w", err)
	}

	// Step 4: Base64 encode both keys (WireGuard expects base64)
	return &KeyPair{
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
	}, nil
}

// ValidateKeyPair checks if a key pair is valid.
// It verifies that both keys are properly base64 encoded and have correct length.
func ValidateKeyPair(kp *KeyPair) error {
	// WireGuard keys are 32 bytes, which encode to 44 base64 characters
	if len(kp.PrivateKey) != 44 {
		return fmt.Errorf("invalid private key length: expected 44, got %d", len(kp.PrivateKey))
	}
	if len(kp.PublicKey) != 44 {
		return fmt.Errorf("invalid public key length: expected 44, got %d", len(kp.PublicKey))
	}

	// Verify base64 encoding
	if _, err := base64.StdEncoding.DecodeString(kp.PrivateKey); err != nil {
		return fmt.Errorf("invalid private key encoding: %w", err)
	}
	if _, err := base64.StdEncoding.DecodeString(kp.PublicKey); err != nil {
		return fmt.Errorf("invalid public key encoding: %w", err)
	}

	return nil
}
