package wireguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// KeyManager handles WireGuard key generation and management.
type KeyManager struct {
	logger *logger.Logger
}

// NewKeyManager creates a new key manager.
func NewKeyManager(log *logger.Logger) *KeyManager {
	if log == nil {
		log = logger.New("info", "text")
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

	// Check if key file exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		km.logger.Info("client key does not exist, generating new key", "path", keyPath)

		// Create directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
			return "", fmt.Errorf("failed to create key directory: %w", err)
		}

		// Generate new private key using wg genkey
		privateKey, err := km.generatePrivateKey()
		if err != nil {
			return "", fmt.Errorf("failed to generate private key: %w", err)
		}

		// Save the key with restricted permissions
		if err := os.WriteFile(keyPath, []byte(privateKey), 0600); err != nil {
			return "", fmt.Errorf("failed to save private key: %w", err)
		}

		km.logger.Info("generated and saved new client key", "path", keyPath)
		return privateKey, nil
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
	if !isValidWireGuardKey(privateKey) {
		return "", fmt.Errorf("invalid private key format in %s", keyPath)
	}

	km.logger.Debug("loaded existing client key", "path", keyPath)
	return privateKey, nil
}

// generatePrivateKey uses wg genkey command to generate a new private key.
func (km *KeyManager) generatePrivateKey() (string, error) {
	cmd := exec.Command("wg", "genkey")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run 'wg genkey': %w", err)
	}

	privateKey := strings.TrimSpace(string(output))

	// Validate the generated key
	if !isValidWireGuardKey(privateKey) {
		return "", fmt.Errorf("generated invalid private key")
	}

	km.logger.Debug("generated new private key")
	return privateKey, nil
}

// GetPublicKey derives the public key from a private key.
func (km *KeyManager) GetPublicKey(privateKey string) (string, error) {
	// Use wg pubkey command to derive public key
	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %w", err)
	}

	publicKey := strings.TrimSpace(string(output))

	// Validate the generated public key
	if !isValidWireGuardKey(publicKey) {
		return "", fmt.Errorf("derived invalid public key")
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

// expandHomeDir expands the ~ in a path to the user's home directory.
func expandHomeDir(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return strings.Replace(path, "~/", home+"/", 1)
	}
	return path
}
