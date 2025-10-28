package provisioner

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestHetzner_readSSHPublicKeys(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	tests := []struct {
		name            string
		sshPublicKey    string
		expectedCount   int
		expectError     bool
		setupTempFile   bool
		tempFileContent string
	}{
		{
			name:          "empty SSH key",
			sshPublicKey:  "",
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:          "direct SSH key content",
			sshPublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG4rT3vTt99Ox5kndS4HmgTrKBT8SKzhK4rhGkEVtQvy test@example.com",
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:            "SSH key from file",
			sshPublicKey:    "", // Will be set to temp file path
			expectedCount:   2,
			expectError:     false,
			setupTempFile:   true,
			tempFileContent: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG4rT3vTt99Ox5kndS4HmgTrKBT8SKzhK4rhGkEVtQvy test1@example.com\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC7vbqajDhA test2@example.com\n",
		},
		{
			name:          "non-existent file",
			sshPublicKey:  "/non/existent/file",
			expectedCount: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &HetznerConfig{
				ServerType:   "cx11",
				Image:        "ubuntu-22.04",
				Location:     "nbg1",
				SSHPublicKey: tt.sshPublicKey,
			}

			// Setup temp file if needed
			if tt.setupTempFile {
				tmpFile, err := os.CreateTemp("", "ssh_key_test_*.pub")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer os.Remove(tmpFile.Name())

				if _, err := tmpFile.WriteString(tt.tempFileContent); err != nil {
					t.Fatalf("Failed to write to temp file: %v", err)
				}
				tmpFile.Close()

				config.SSHPublicKey = tmpFile.Name()
			}

			h := &Hetzner{
				config: config,
				logger: logger,
			}

			keys, err := h.readSSHPublicKeys()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(keys) != tt.expectedCount {
				t.Errorf("Expected %d keys, got %d", tt.expectedCount, len(keys))
			}

			// Verify keys are valid SSH key format
			for _, key := range keys {
				if !strings.HasPrefix(key, "ssh-") && !strings.HasPrefix(key, "ecdsa-") && !strings.HasPrefix(key, "ed25519-") {
					t.Errorf("Invalid SSH key format: %s", key)
				}
			}
		})
	}
}

func TestHetzner_generateCloudInitWithSSHKeys(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create temp SSH key file
	tmpFile, err := os.CreateTemp("", "ssh_key_test_*.pub")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	sshKeyContent := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG4rT3vTt99Ox5kndS4HmgTrKBT8SKzhK4rhGkEVtQvy test@example.com"
	if _, err := tmpFile.WriteString(sshKeyContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	config := &HetznerConfig{
		ServerType:   "cx11",
		Image:        "ubuntu-22.04",
		Location:     "nbg1",
		SSHPublicKey: tmpFile.Name(),
	}

	h := &Hetzner{
		config: config,
		logger: logger,
	}

	// Generate test keys
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keys: %v", err)
	}

	// Generate cloud-init
	cloudInit, err := h.generateCloudInit(keys)
	if err != nil {
		t.Fatalf("Failed to generate cloud-init: %v", err)
	}

	// Verify SSH key is included
	if !strings.Contains(cloudInit, sshKeyContent) {
		t.Errorf("SSH key not found in cloud-init template")
	}

	// Verify WireGuard keys are included
	if !strings.Contains(cloudInit, keys.PrivateKey) {
		t.Errorf("WireGuard private key not found in cloud-init template")
	}

	if !strings.Contains(cloudInit, keys.PublicKey) {
		t.Errorf("WireGuard public key not found in cloud-init template")
	}

	// Verify SSH configuration is present
	if !strings.Contains(cloudInit, "ssh_authorized_keys:") {
		t.Errorf("SSH authorized keys section not found in cloud-init template")
	}

	// Verify user configuration is present
	if !strings.Contains(cloudInit, "users:") {
		t.Errorf("Users section not found in cloud-init template")
	}

	// Verify root user configuration
	if !strings.Contains(cloudInit, "name: root") {
		t.Errorf("Root user configuration not found in cloud-init template")
	}

	// Verify root account is unlocked
	if !strings.Contains(cloudInit, "lock_passwd: false") {
		t.Errorf("Root account unlock configuration not found in cloud-init template")
	}

	// Verify disable_root is false
	if !strings.Contains(cloudInit, "disable_root: false") {
		t.Errorf("disable_root: false not found in cloud-init template")
	}

	// Verify MTU setting is present
	if !strings.Contains(cloudInit, "MTU = 1420") {
		t.Errorf("MTU = 1420 not found in cloud-init template")
	}
}
