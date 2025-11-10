package wireguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
)

// KeyManager handles WireGuard key generation and management.
type KeyManager struct {
	logger *logger.Logger
}

// NewKeyManager creates a new key manager.
func NewKeyManager(log *logger.Logger) *KeyManager {
	if log == nil {
		log = logger.NewDevelopment("keymanager")
	}

	return &KeyManager{
		logger: log,
	}
}

// LoadOrCreateKey loads an existing key or creates a new one.
func (km *KeyManager) LoadOrCreateKey(keyPath string) (string, error) {
	// Expand the path if it contains ~
	keyPath = expandHomeDir(keyPath)
	km.logger.Debug("loading or creating client key", "path", keyPath)

	// If key exists, reuse LoadExistingKey to avoid duplicating validation/reading logic
	if _, err := os.Stat(keyPath); err == nil {
		km.logger.Debug("client key exists, loading", "path", keyPath)
		return km.LoadExistingKey(keyPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to check key file: %w", err)
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return "", fmt.Errorf("failed to create key directory: %w", err)
	}

	// Generate new key pair
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", fmt.Errorf("failed to generate key pair: %w", err)
	}
	privateKey := keyPair.PrivateKey

	// Write atomically: create temp file in same dir, set permissions, then rename
	dir := filepath.Dir(keyPath)
	tmpFile, err := os.CreateTemp(dir, "wgkey-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for key: %w", err)
	}
	// Ensure cleanup on error
	tmpName := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.WriteString(privateKey + "\n"); err != nil {
		return "", fmt.Errorf("failed to write private key to temp file: %w", err)
	}

	if err := tmpFile.Chmod(0600); err != nil {
		return "", fmt.Errorf("failed to set permissions on temp key file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp key file: %w", err)
	}

	if err := os.Rename(tmpName, keyPath); err != nil {
		return "", fmt.Errorf("failed to move temp key file into place: %w", err)
	}

	km.logger.Info("generated and saved new client key", "path", keyPath)
	return privateKey, nil
}

// GetPublicKey derives the public key from a private key.
func (km *KeyManager) GetPublicKey(privateKey string) (string, error) {
	publicKey, err := crypto.DerivePublicKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %w", err)
	}
	km.logger.Debug("derived public key from private key")
	return publicKey, nil
}

// DeleteKey removes the key file.
func (km *KeyManager) DeleteKey(keyPath string) error {
	// Expand the path if it contains ~
	keyPath = expandHomeDir(keyPath)
	km.logger.Info("deleting client key", "path", keyPath)

	err := os.Remove(keyPath)
	if err != nil {
		km.logger.Error("failed to delete key file", "path", keyPath, "error", err)
		return fmt.Errorf("failed to delete key file: %w", err)
	}

	km.logger.Info("successfully deleted client key", "path", keyPath)
	return nil
}

// LoadExistingKey loads an existing private key from the specified path.
func (km *KeyManager) LoadExistingKey(keyPath string) (string, error) {
	// Expand the path if it contains ~
	keyPath = expandHomeDir(keyPath)
	km.logger.Debug("loading existing key", "path", keyPath)

	// Check if key file exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return "", fmt.Errorf("key file does not exist: %s", keyPath)
	} else if err != nil {
		return "", fmt.Errorf("failed to check key file: %w", err)
	}

	// Load existing key
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read private key: %w", err)
	}

	privateKey := strings.TrimSpace(string(keyBytes))

	// Validate key format
	if !crypto.IsValidWireGuardKey(privateKey) {
		return "", fmt.Errorf("invalid private key format in %s", keyPath)
	}

	km.logger.Debug("loaded existing key", "path", keyPath)
	return privateKey, nil
}

// GenerateKeyPair generates a new WireGuard key pair.
func (km *KeyManager) GenerateKeyPair() (privateKey, publicKey string, err error) {
	km.logger.Debug("generating new key pair")
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate key pair: %w", err)
	}
	km.logger.Debug("generated new key pair")
	return keyPair.PrivateKey, keyPair.PublicKey, nil
}

// SavePrivateKey saves a private key to the specified path with secure permissions.
func (km *KeyManager) SavePrivateKey(privateKey, keyPath string) error {
	// Expand the path if it contains ~
	keyPath = expandHomeDir(keyPath)
	km.logger.Debug("saving private key", "path", keyPath)

	// Validate key format before saving
	if !crypto.IsValidWireGuardKey(privateKey) {
		return fmt.Errorf("invalid private key format")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return fmt.Errorf("failed to create key directory: %w", err)
	}

	// Save the key with restricted permissions (atomic write similar to LoadOrCreateKey)
	tmpFile, err := os.CreateTemp(filepath.Dir(keyPath), "wgkey-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for key: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.WriteString(privateKey + "\n"); err != nil {
		return fmt.Errorf("failed to write private key to temp file: %w", err)
	}

	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("failed to set permissions on temp key file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp key file: %w", err)
	}

	if err := os.Rename(tmpName, keyPath); err != nil {
		return fmt.Errorf("failed to move temp key file into place: %w", err)
	}

	km.logger.Info("saved private key", "path", keyPath)
	return nil
}

// ValidatePrivateKey validates a WireGuard private key format.
func (km *KeyManager) ValidatePrivateKey(privateKey string) error {
	if !crypto.IsValidWireGuardKey(privateKey) {
		return fmt.Errorf("invalid WireGuard private key format")
	}
	return nil
}

// ValidatePublicKey validates a WireGuard public key format.
func (km *KeyManager) ValidatePublicKey(publicKey string) error {
	if !crypto.IsValidWireGuardKey(publicKey) {
		return fmt.Errorf("invalid WireGuard public key format")
	}
	return nil
}

// expandHomeDir expands the ~ in a path to the user's home directory.
func expandHomeDir(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return strings.Replace(path, "~/", home+"/", 1)
	}
	return path
}
