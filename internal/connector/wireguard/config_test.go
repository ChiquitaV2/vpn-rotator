package wireguard

import (
	"os"
	"strings"
	"testing"

	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

func TestConfigGenerator_GenerateConfig(t *testing.T) {
	// Create a temporary directory for test keys
	tmpDir, err := os.MkdirTemp("", "wireguard_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := logger.New("info", "text")
	cg := NewConfigGenerator(logger)

	// Test parameters
	clientKeyPath := tmpDir + "/client.key"
	serverPublicKey := "test-server-public-key=="
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
