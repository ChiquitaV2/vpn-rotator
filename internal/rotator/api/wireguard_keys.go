package api

import (
	"fmt"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// WireGuardKeyPair represents a WireGuard key pair
type WireGuardKeyPair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// GenerateWireGuardKeyPair generates a new WireGuard key pair
func GenerateWireGuardKeyPair() (*WireGuardKeyPair, error) {
	// Delegate key generation to the new centralized crypto package
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair using crypto package: %w", err)
	}

	// Map the crypto.KeyPair to the local api.WireGuardKeyPair for API compatibility
	return &WireGuardKeyPair{
		PrivateKey: keyPair.PrivateKey,
		PublicKey:  keyPair.PublicKey,
	}, nil
}
