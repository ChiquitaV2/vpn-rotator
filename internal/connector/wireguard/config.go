package wireguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	"github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// ConfigGenerator generates and applies WireGuard configurations.
type ConfigGenerator struct {
	clientKeyManager *KeyManager
	logger           *logger.Logger
}

// NewConfigGenerator creates a new configuration generator.
func NewConfigGenerator(log *logger.Logger) *ConfigGenerator {
	if log == nil {
		log = logger.NewDevelopment("wireguard")
	}

	return &ConfigGenerator{
		clientKeyManager: NewKeyManager(log),
		logger:           log,
	}
}

// GenerateConfig creates a WireGuard configuration string.
func (cg *ConfigGenerator) GenerateConfig(clientKeyPath, serverPublicKey, serverIP string, serverPort int, allowedIPs, dns string) (string, error) {
	// Load or generate client private key
	privateKey, err := cg.clientKeyManager.LoadOrCreateKey(clientKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to load/create client key: %w", err)
	}

	// Generate the WireGuard config content
	config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.100.0.2/32
DNS = %s
MTU = 1420

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = %s
PersistentKeepalive = 25
`,
		privateKey,
		dns,
		serverPublicKey,
		serverIP,
		serverPort,
		allowedIPs,
	)

	cg.logger.Debug("generated WireGuard config", "server_ip", serverIP, "server_port", serverPort)
	return config, nil
}

// WriteConfigFile writes the configuration to a file.
func (cg *ConfigGenerator) WriteConfigFile(configContent, configPath string) error {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write the configuration to file
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	cg.logger.Debug("wrote WireGuard config file", "path", configPath)
	return nil
}

// ApplyConfig applies the WireGuard configuration using wg-quick up.
func (cg *ConfigGenerator) ApplyConfig(configPath, interfaceName string) (string, error) {
	cg.logger.Info("applying WireGuard configuration", "interface", interfaceName, "config_path", configPath)

	cmd := exec.Command("wg-quick", "up", configPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("WG_INTERFACE_NAME=%s", interfaceName), // Custom interface name
		"WG_ENDPOINT_RESOLUTION_RETRIES=5",                 // Retry endpoint resolution
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cg.logger.Error("failed to apply WireGuard config", "interface", interfaceName, "error", err, "output", string(output))
		return "", fmt.Errorf("failed to apply WireGuard config: %w, output: %s", err, string(output))
	}

	// read interface name from output (e.g. "Interface for wg-wg0 is utun6")
	actualIf := ""
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Interface for ") && strings.Contains(line, " is ") {
			parts := strings.Split(line, " is ")
			if len(parts) >= 2 {
				cand := strings.TrimSpace(parts[len(parts)-1])
				if cand != "" {
					actualIf = cand
					break
				}
			}
		}
	}

	if actualIf != "" && actualIf != interfaceName {
		cg.logger.Info("WireGuard interface name changed during setup", "requested", interfaceName, "effective", actualIf)
		return actualIf, nil
	}

	cg.logger.Info("successfully applied WireGuard config", "interface", interfaceName)
	return interfaceName, nil
}

// RemoveConfig removes the WireGuard configuration using wg-quick down.
func (cg *ConfigGenerator) RemoveConfig(configPath, interfaceName string) error {
	cg.logger.Info("removing WireGuard configuration", "interface", interfaceName, "config_path", configPath)

	cmd := exec.Command("wg-quick", "down", configPath)
	//cmd = exec.Command("wg-quick", "down", interfaceName)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cg.logger.Error("failed to remove WireGuard config", "interface", interfaceName, "error", err, "output", string(output))
		return fmt.Errorf("failed to remove WireGuard config: %w, output: %s", err, string(output))
	}

	cg.logger.Info("successfully removed WireGuard config", "interface", interfaceName)
	return nil
}

// ValidateConfig validates the WireGuard configuration before applying.
func (cg *ConfigGenerator) ValidateConfig(configContent string) error {
	cg.logger.Debug("validating WireGuard configuration")

	lines := strings.Split(configContent, "\n")

	hasInterface := false
	hasPeer := false
	privateKeyValid := false
	publicKeyValid := false
	endpointValid := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[Interface]") {
			hasInterface = true
		} else if strings.HasPrefix(line, "[Peer]") {
			hasPeer = true
		} else if strings.HasPrefix(line, "PrivateKey") {
			privateKey := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
			privateKeyValid = crypto.IsValidWireGuardKey(privateKey)
		} else if strings.HasPrefix(line, "PublicKey") {
			publicKey := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
			publicKeyValid = crypto.IsValidWireGuardKey(publicKey)
		} else if strings.HasPrefix(line, "Endpoint") {
			endpoint := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
			endpointValid = isValidEndpoint(endpoint)
		}
	}

	if !hasInterface || !hasPeer {
		err := fmt.Errorf("configuration missing required sections")
		cg.logger.Error("configuration validation failed", "error", err)
		return err
	}

	if !privateKeyValid {
		err := fmt.Errorf("invalid private key format")
		cg.logger.Error("configuration validation failed", "error", err)
		return err
	}

	if !publicKeyValid {
		err := fmt.Errorf("invalid public key format")
		cg.logger.Error("configuration validation failed", "error", err)
		return err
	}

	if !endpointValid {
		err := fmt.Errorf("invalid endpoint format")
		cg.logger.Error("configuration validation failed", "error", err)
		return err
	}

	cg.logger.Debug("configuration validation passed")
	return nil
}

// isValidEndpoint checks if an endpoint is a valid IP:port format
func isValidEndpoint(endpoint string) bool {
	parts := strings.Split(endpoint, ":")
	if len(parts) != 2 {
		return false
	}

	ip := parts[0]
	port := parts[1]

	// Simple validation for IP (could be extended)
	if len(ip) < 7 || len(ip) > 15 { // IPv4 range
		return false
	}

	// Validate port
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}

	portNum := 0
	fmt.Sscanf(port, "%d", &portNum)
	return portNum > 0 && portNum < 65536
}

// InterfaceExists checks if a WireGuard interface exists
func (cg *ConfigGenerator) InterfaceExists(interfaceName string) bool {
	cmd := exec.Command("wg", "show", interfaceName)
	err := cmd.Run()
	return err == nil
}

// UpdateConfigField updates a specific field in a WireGuard configuration
func (cg *ConfigGenerator) UpdateConfigField(config, field, value string) string {
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), field) {
			lines[i] = fmt.Sprintf("%s = %s", field, value)
			break
		}
	}
	return strings.Join(lines, "\n")
}
