package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"

	"golang.org/x/crypto/curve25519"
)

// KeyPair represents a WireGuard key pair (private and public keys).
type KeyPair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// GenerateKeyPair generates a new WireGuard key pair using native crypto.
func GenerateKeyPair() (*KeyPair, error) {
	privateKeyBytes := make([]byte, 32)
	if _, err := rand.Read(privateKeyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes for private key: %w", err)
	}

	// Clamp the key according to WireGuard specification.
	clampPrivateKey(privateKeyBytes)

	publicKeyBytes, err := curve25519.X25519(privateKeyBytes, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to generate public key: %w", err)
	}

	return &KeyPair{
		PrivateKey: base64.StdEncoding.EncodeToString(privateKeyBytes),
		PublicKey:  base64.StdEncoding.EncodeToString(publicKeyBytes),
	}, nil
}

// DerivePublicKey derives a public key from a given private key.
func DerivePublicKey(privateKey string) (string, error) {
	privateKeyBytes, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("private key is not valid base64: %w", err)
	}
	if len(privateKeyBytes) != 32 {
		return "", fmt.Errorf("private key has incorrect length: expected 32 bytes")
	}

	// Clamp the key according to WireGuard specification before deriving the public key.
	clampPrivateKey(privateKeyBytes)

	publicKeyBytes, err := curve25519.X25519(privateKeyBytes, curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(publicKeyBytes), nil
}

// clampPrivateKey applies the clamping function to a private key as specified by WireGuard.
func clampPrivateKey(key []byte) {
	key[0] &= 248
	key[31] &= 127
	key[31] |= 64
}

// IsValidWireGuardKey validates the format of a WireGuard key.
// It checks for correct length and valid base64 encoding.
func IsValidWireGuardKey(key string) bool {
	// WireGuard keys are base64-encoded 32-byte values, which results in 44 characters.
	if len(key) != 44 {
		return false
	}

	// Use a regex for a quick check of the character set.
	validBase64 := regexp.MustCompile(`^[A-Za-z0-9+/]+=*$`)
	if !validBase64.MatchString(key) {
		return false
	}

	// The ultimate test is to decode it.
	_, err := base64.StdEncoding.DecodeString(key)
	return err == nil
}
