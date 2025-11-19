package wireguard

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	"github.com/chiquitav2/vpn-rotator/pkg/logger"
)

func TestConfigGenerator_GenerateConfig(t *testing.T) {
	// Create a temporary directory for test keys
	tmpDir, err := os.MkdirTemp("", "wireguard_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := logger.NewDevelopment("config_test")
	cg := NewConfigGenerator(logger)

	// Test parameters
	clientKeyPath := tmpDir + "/client.key"
	serverPublicKey := "6M/drTjVVAMzVkNR+hSdw4RRIT1EbqDquukqM2+0HEE="
	serverIP := "192.168.1.100"
	serverPort := 51820
	allowedIPs := "0.0.0.0/0"
	dns := "1.1.1.1"

	// Generate config
	config, err := cg.GenerateConfig(clientKeyPath, serverPublicKey, serverIP, serverPort, allowedIPs, dns)
	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	// Verify MTU setting is present
	if !strings.Contains(config, "MTU = 1420") {
		t.Errorf("MTU = 1420 not found in generated config")
		t.Logf("Generated config:\n%s", config)
	}

	// Verify other required settings
	if !strings.Contains(config, "[Interface]") {
		t.Errorf("[Interface] section not found in generated config")
	}

	if !strings.Contains(config, "[Peer]") {
		t.Errorf("[Peer] section not found in generated config")
	}

	if !strings.Contains(config, serverPublicKey) {
		t.Errorf("Server public key not found in generated config")
	}

	if !strings.Contains(config, "Address = 10.100.0.2/32") {
		t.Errorf("Client address not found in generated config")
	}

	if !strings.Contains(config, "DNS = "+dns) {
		t.Errorf("DNS setting not found in generated config")
	}

	if !strings.Contains(config, "PersistentKeepalive = 25") {
		t.Errorf("PersistentKeepalive setting not found in generated config")
	}
}

func TestConfigGenerator_ValidateConfig(t *testing.T) {
	logger := logger.NewDevelopment("config_test")
	cg := NewConfigGenerator(logger)

	validKeyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("could not generate key pair: %v", err)
	}

	t.Run("valid config", func(t *testing.T) {
		config := fmt.Sprintf(`
[Interface]
PrivateKey = %s
[Peer]
PublicKey = %s
Endpoint = 1.2.3.4:51820
`, validKeyPair.PrivateKey, validKeyPair.PublicKey)
		err := cg.ValidateConfig(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("missing interface section", func(t *testing.T) {
		config := `
[Peer]
PublicKey = somekey
`
		err := cg.ValidateConfig(config)
		if err == nil {
			t.Error("expected an error, got nil")
		}
	})

	t.Run("missing peer section", func(t *testing.T) {
		config := `
[Interface]
PrivateKey = somekey
`
		err := cg.ValidateConfig(config)
		if err == nil {
			t.Error("expected an error, got nil")
		}
	})

	t.Run("invalid private key", func(t *testing.T) {
		config := fmt.Sprintf(`
[Interface]
PrivateKey = invalid-key
[Peer]
PublicKey = %s
Endpoint = 1.2.3.4:51820
`, validKeyPair.PublicKey)
		err := cg.ValidateConfig(config)
		if err == nil {
			t.Error("expected an error for invalid private key")
		}
	})

	t.Run("invalid public key", func(t *testing.T) {
		config := fmt.Sprintf(`
[Interface]
PrivateKey = %s
[Peer]
PublicKey = invalid-key
Endpoint = 1.2.3.4:51820
`, validKeyPair.PrivateKey)
		err := cg.ValidateConfig(config)
		if err == nil {
			t.Error("expected an error for invalid public key")
		}
	})

	t.Run("invalid endpoint", func(t *testing.T) {
		config := fmt.Sprintf(`
[Interface]
PrivateKey = %s
[Peer]
PublicKey = %s
Endpoint = not-an-endpoint
`, validKeyPair.PrivateKey, validKeyPair.PublicKey)
		err := cg.ValidateConfig(config)
		if err == nil {
			t.Error("expected an error for invalid endpoint")
		}
	})
}
