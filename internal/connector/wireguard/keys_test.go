package wireguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

func TestKeyManager_LoadOrCreateKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "key-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.NewDevelopment("keys_test")
	km := NewKeyManager(log)
	keyPath := filepath.Join(tmpDir, "test.key")

	// 1. Key doesn't exist, should be created
	privateKey, err := km.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey failed: %v", err)
	}
	if !crypto.IsValidWireGuardKey(privateKey) {
		t.Error("created key is not a valid wireguard key")
	}

	_, err = os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file should have been created, but stat failed: %v", err)
	}

	// 2. Key exists, should be loaded
	loadedKey, err := km.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey failed on existing key: %v", err)
	}
	if privateKey != loadedKey {
		t.Errorf("loaded key does not match created key")
	}
}

func TestKeyManager_GetPublicKey(t *testing.T) {
	log := logger.NewDevelopment("keys_test")
	km := NewKeyManager(log)

	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKey, err := km.GetPublicKey(keyPair.PrivateKey)
	if err != nil {
		t.Fatalf("GetPublicKey failed: %v", err)
	}
	if publicKey != keyPair.PublicKey {
		t.Errorf("derived public key does not match")
	}
}

func TestKeyManager_SaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "key-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log := logger.NewDevelopment("keys_test")
	km := NewKeyManager(log)
	keyPath := filepath.Join(tmpDir, "my.key")

	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	// Save the private key
	if err := km.SavePrivateKey(keyPair.PrivateKey, keyPath); err != nil {
		t.Fatalf("SavePrivateKey failed: %v", err)
	}

	// Load it back
	loadedKey, err := km.LoadExistingKey(keyPath)
	if err != nil {
		t.Fatalf("LoadExistingKey failed: %v", err)
	}
	if loadedKey != keyPair.PrivateKey {
		t.Errorf("loaded key does not match saved key")
	}

	// Check permissions
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat failed on key file: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(0600) {
		t.Errorf("key file has wrong permissions: got %v, want %v", info.Mode().Perm(), os.FileMode(0600))
	}
}
