package connector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/connector/wireguard"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// We'll use the config package's Config type instead of defining our own

// Connector provides a simplified VPN connection interface
type Connector struct {
	apiClient  *client.Client
	configGen  *wireguard.ConfigGenerator
	keyManager *wireguard.KeyManager
	logger     *logger.Logger

	// Configuration
	config *config.Config

	// State
	peerID         string
	tempConfigPath string
	connected      bool
}

// NewConnector creates a new simplified connector
func NewConnector(cfg *config.Config, log *logger.Logger) *Connector {
	if log == nil {
		log = logger.NewDevelopment("connector")
	}

	if cfg.KeyPath == "" {
		cfg.KeyPath = "~/.vpn-rotator/client.key"
	}
	if cfg.Interface == "" {
		cfg.Interface = "wg0"
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 15
	}
	if cfg.AllowedIPs == "" {
		cfg.AllowedIPs = "0.0.0.0/0"
	}
	if cfg.DNS == "" {
		cfg.DNS = "9.9.9.9,149.112.112.112"
	}

	// Expand home directory in key path
	cfg.KeyPath = expandPath(cfg.KeyPath)

	apiClient := client.NewClient(cfg.APIURL, log)

	return &Connector{
		apiClient:  apiClient,
		configGen:  wireguard.NewConfigGenerator(log),
		keyManager: wireguard.NewKeyManager(log),
		logger:     log,
		config:     cfg,
	}
}

// SetProvisioningWaitConfig configures the provisioning wait behavior
func (c *Connector) SetProvisioningWaitConfig(maxWaits int, statusCheckInterval time.Duration) {
	c.apiClient.SetProvisioningWaitConfig(maxWaits, statusCheckInterval)
}

// Connect establishes a VPN connection with automatic key management
func (c *Connector) Connect(ctx context.Context) error {
	c.logger.Info("starting VPN connection process")
	c.logger.Info("establishing VPN connection", "interface", c.config.Interface)

	// Prepare connection request with intelligent key handling
	connectReq, err := c.prepareConnectRequest()
	if err != nil {
		return fmt.Errorf("failed to prepare connection request: %w", err)
	}

	// Connect to the VPN service
	connectResp, err := c.apiClient.ConnectPeer(ctx, connectReq)
	if err != nil {
		return fmt.Errorf("failed to connect peer: %w", err)
	}

	// Handle server-generated keys if needed
	if err := c.handleServerKeys(connectResp); err != nil {
		return fmt.Errorf("failed to handle server keys: %w", err)
	}

	// Generate and apply WireGuard configuration
	var configpath string
	if configpath, err = c.applyWireGuardConfig(connectResp); err != nil {
		return fmt.Errorf("failed to apply WireGuard config: %w", err)
	}

	c.logger.Info("vpn configuration applied", "config_path", configpath)
	// Save connection state
	c.peerID = connectResp.PeerID
	c.tempConfigPath = configpath
	c.connected = true

	if err := c.saveConnectionState(); err != nil {
		c.logger.Warn("failed to save connection state", "error", err)
	}

	c.logger.Info("VPN connection established",
		"peer_id", connectResp.PeerID,
		"server_ip", connectResp.ServerIP,
		"client_ip", connectResp.ClientIP,
	)

	return nil
}

// ConnectWithProvisioningFeedback establishes a VPN connection with user-friendly provisioning feedback
func (c *Connector) ConnectWithProvisioningFeedback(ctx context.Context) error {
	c.logger.Info("starting VPN connection process with provisioning feedback")

	// Prepare connection request with intelligent key handling
	connectReq, err := c.prepareConnectRequest()
	if err != nil {
		return fmt.Errorf("failed to prepare connection request: %w", err)
	}

	fmt.Printf("Connecting to VPN...\n")

	// Connect to the VPN service with enhanced error handling
	connectResp, err := c.apiClient.ConnectPeer(ctx, connectReq)
	if err != nil {
		// Check if this is a provisioning error
		if provErr, ok := err.(*client.ProvisioningInProgressError); ok {
			fmt.Printf("\n Node provisioning is in progress\n")
			fmt.Printf("   Message: %s\n", provErr.Message)
			fmt.Printf("   Estimated wait: %d seconds\n", provErr.EstimatedWait)
			fmt.Printf("   The system will automatically retry when ready\n\n")

			// The client has already handled the waiting, so this means it timed out
			return fmt.Errorf("provisioning took longer than expected: %s", provErr.Message)
		}
		return fmt.Errorf("failed to connect peer: %w", err)
	}

	fmt.Printf("✅ Connection established successfully!\n")

	// Handle server-generated keys if needed
	if err := c.handleServerKeys(connectResp); err != nil {
		return fmt.Errorf("failed to handle server keys: %w", err)
	}

	// Generate and apply WireGuard configuration
	var configpath string
	if configpath, err = c.applyWireGuardConfig(connectResp); err != nil {
		return fmt.Errorf("failed to apply WireGuard config: %w", err)
	}

	c.logger.Info("vpn configuration applied", "config_path", configpath)

	// Save connection state
	c.peerID = connectResp.PeerID
	c.tempConfigPath = configpath
	c.connected = true

	if err := c.saveConnectionState(); err != nil {
		c.logger.Warn("failed to save connection state", "error", err)
	}

	c.logger.Info("VPN connection established",
		"peer_id", connectResp.PeerID,
		"server_ip", connectResp.ServerIP,
		"client_ip", connectResp.ClientIP,
	)

	return nil
}

// ConnectWithoutWait attempts to connect without waiting for provisioning
func (c *Connector) ConnectWithoutWait(ctx context.Context) error {
	c.logger.Info("attempting VPN connection without provisioning wait")

	// Prepare connection request
	connectReq, err := c.prepareConnectRequest()
	if err != nil {
		return fmt.Errorf("failed to prepare connection request: %w", err)
	}

	// Try to connect without waiting
	connectResp, err := c.apiClient.ConnectPeerWithoutWait(ctx, connectReq)
	if err != nil {
		return err // Return the error as-is (could be ProvisioningInProgressError)
	}

	// Handle successful connection
	if err := c.handleServerKeys(connectResp); err != nil {
		return fmt.Errorf("failed to handle server keys: %w", err)
	}

	var configpath string
	if configpath, err = c.applyWireGuardConfig(connectResp); err != nil {
		return fmt.Errorf("failed to apply WireGuard config: %w", err)
	}

	c.peerID = connectResp.PeerID
	c.tempConfigPath = configpath
	c.connected = true

	if err := c.saveConnectionState(); err != nil {
		c.logger.Warn("failed to save connection state", "error", err)
	}

	return nil
}

// Disconnect terminates the VPN connection
func (c *Connector) Disconnect() error {
	c.logger.Info("disconnecting VPN", "interface", c.config.Interface)

	// Remove peer from server if we have a peer ID
	if c.peerID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		disconnectReq := &api.DisconnectRequest{PeerID: c.peerID}
		if _, err := c.apiClient.DisconnectPeer(ctx, disconnectReq); err != nil {
			c.logger.Warn("failed to disconnect peer from server", "error", err)
		}
	}

	// Remove local WireGuard configuration
	if err := c.configGen.RemoveConfig(c.tempConfigPath, c.config.Interface); err != nil {
		return fmt.Errorf("failed to remove WireGuard configuration: %w", err)
	}

	// Clean up connection state
	c.cleanupConnectionState()
	c.connected = false
	c.peerID = ""

	c.logger.Info("VPN disconnected successfully")
	return nil
}

// Reconnect performs a full reconnection from existing state
func (c *Connector) Reconnect(ctx context.Context) error {
	//check is file exists
	fst, err := os.Stat(c.getStateFile())
	if err != nil || fst.IsDir() {
		return fmt.Errorf("no existing connection state found, cannot reconnect: %w", err)
	}

	c.logger.Info("reconnecting VPN", "interface", c.config.Interface)

	// Disconnect if connected
	if c.connected {
		if err := c.Disconnect(); err != nil {
			return fmt.Errorf("failed to disconnect before reconnect: %w", err)
		}

		// Wait a moment for cleanup
		time.Sleep(1 * time.Second)
	}

	var interfaceIf string
	if interfaceIf, err = c.configGen.ApplyConfig(c.tempConfigPath, c.config.Interface); err != nil {
		return fmt.Errorf("failed to reconnect WireGuard config: %w", err)
	}
	if interfaceIf != "" && interfaceIf != c.config.Interface {
		c.config.Interface = interfaceIf
	}
	c.connected = true

	c.logger.Info("VPN reconnected successfully", "peer_id", c.peerID, "interface", c.config.Interface)
	return c.Connect(ctx)
}

// IsConnected returns whether the VPN is currently connected
func (c *Connector) IsConnected() bool {
	return c.connected && c.configGen.InterfaceExists(c.config.Interface)
}

// GetPeerID returns the current peer ID
func (c *Connector) GetPeerID() string {
	return c.peerID
}

// MonitorRotation starts monitoring for node rotations
func (c *Connector) MonitorRotation(ctx context.Context) error {
	if !c.connected || c.peerID == "" {
		return fmt.Errorf("not connected - cannot monitor rotation")
	}

	ticker := time.NewTicker(time.Duration(c.config.PollInterval) * time.Minute)
	defer ticker.Stop()

	c.logger.Info("starting rotation monitoring", "peer_id", c.peerID, "interval", c.config.PollInterval)

	var lastNodeID string

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("rotation monitoring stopped")
			return ctx.Err()
		case <-ticker.C:
			// Check peer status
			peerInfo, err := c.apiClient.GetPeerStatus(ctx, c.peerID)
			if err != nil {
				c.logger.Warn("failed to get peer status", "error", err)
				continue
			}

			// Check for node migration
			if lastNodeID != "" && lastNodeID != peerInfo.NodeID {
				c.logger.Info("peer migrated to new node",
					"peer_id", peerInfo.ID,
					"old_node", lastNodeID,
					"new_node", peerInfo.NodeID,
				)
				// In a full implementation, we might need to update configuration
			}

			lastNodeID = peerInfo.NodeID
		}
	}
}

// prepareConnectRequest creates a connection request with intelligent key handling
func (c *Connector) prepareConnectRequest() (*api.ConnectRequest, error) {
	connectReq := &api.ConnectRequest{}

	if c.config.GenerateKeys {
		// Request server-side key generation
		connectReq.GenerateKeys = true
		c.logger.Debug("requesting server-side key generation")
		return connectReq, nil
	}

	// Try to use existing key or create new one
	privateKey, err := c.keyManager.LoadOrCreateKey(c.config.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load or create key: %w", err)
	}

	// Derive public key
	publicKey, err := c.keyManager.GetPublicKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive public key: %w", err)
	}

	connectReq.PublicKey = &publicKey
	c.logger.Debug("using local key", "key_path", c.config.KeyPath)

	return connectReq, nil
}

// handleServerKeys processes server-generated keys
func (c *Connector) handleServerKeys(resp *api.ConnectResponse) error {
	if !c.config.GenerateKeys || resp.ClientPrivateKey == nil {
		return nil
	}

	c.logger.Info("saving server-generated key", "key_path", c.config.KeyPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(c.config.KeyPath), 0700); err != nil {
		return fmt.Errorf("failed to create key directory: %w", err)
	}

	// Save the server-generated key
	if err := c.keyManager.SavePrivateKey(*resp.ClientPrivateKey, c.config.KeyPath); err != nil {
		return fmt.Errorf("failed to save server-generated key: %w", err)
	}

	return nil
}

// applyWireGuardConfig generates and applies the WireGuard configuration
func (c *Connector) applyWireGuardConfig(resp *api.ConnectResponse) (string, error) {
	// Generate configuration
	configContent, err := c.configGen.GenerateConfig(
		c.config.KeyPath,
		resp.ServerPublicKey,
		resp.ServerIP,
		resp.ServerPort,
		c.config.AllowedIPs,
		c.config.DNS,
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate config: %w", err)
	}

	// Override with server-provided values if available
	if resp.ClientIP != "" {
		configContent = c.updateClientIP(configContent, resp.ClientIP)
	}
	if len(resp.DNS) > 0 {
		configContent = c.updateDNS(configContent, resp.DNS)
	}
	if len(resp.AllowedIPs) > 0 {
		configContent = c.updateAllowedIPs(configContent, resp.AllowedIPs)
	}

	// Validate configuration
	if err := c.configGen.ValidateConfig(configContent); err != nil {
		return "", fmt.Errorf("invalid WireGuard config: %w", err)
	}

	// Write and apply configuration
	c.tempConfigPath = fmt.Sprintf("/tmp/wg-%s.conf", c.config.Interface)
	if err := c.configGen.WriteConfigFile(configContent, c.tempConfigPath); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}
	var interfaceIf string
	if interfaceIf, err = c.configGen.ApplyConfig(c.tempConfigPath, c.config.Interface); err != nil {
		return "", fmt.Errorf("failed to apply WireGuard config: %w", err)
	}
	if interfaceIf != "" && interfaceIf != c.config.Interface {
		if err = os.Remove(c.tempConfigPath); err != nil && !os.IsNotExist(err) {
			c.logger.Warn("failed to remove old config file", "error", err)
		}
		c.config.Interface = interfaceIf
		// Update tempConfigPath to reflect actual interface used
		c.tempConfigPath = fmt.Sprintf("/tmp/wg-%s.conf", c.config.Interface)
		//write the config file again with the new interface name
		if err = c.configGen.WriteConfigFile(configContent, c.tempConfigPath); err != nil {
			return "", fmt.Errorf("failed to write config file: %w", err)
		}
	}

	return c.tempConfigPath, nil
}

// saveConnectionState saves the current connection state
func (c *Connector) saveConnectionState() error {
	if c.peerID == "" {
		return nil
	}

	stateFile := c.getStateFile()
	if err := os.MkdirAll(filepath.Dir(stateFile), 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	if err := os.WriteFile(stateFile, []byte(fmt.Sprintf(c.peerID, ":", c.tempConfigPath)), 0600); err != nil {
		return fmt.Errorf("failed to write connection state: %w", err)
	}

	return nil
}

// cleanupConnectionState removes the connection state file
func (c *Connector) cleanupConnectionState() {
	stateFile := c.getStateFile()
	if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
		c.logger.Warn("failed to remove connection state file", "error", err)
	}
}

// LoadConnectionState attempts to restore connection state
func (c *Connector) LoadConnectionState() error {
	stateFile := c.getStateFile()
	content, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file is OK
		}
		return fmt.Errorf("failed to read connection state: %w", err)
	}
	states := strings.Split(string(content), ":")
	if len(states) != 2 {
		return fmt.Errorf("invalid connection state format")
	}
	c.tempConfigPath = states[1]
	c.peerID = states[0]
	//get interface name from tempConfigPath, only if it exists
	configInterface := filepath.Base(c.tempConfigPath)
	configInterface = strings.TrimPrefix(strings.TrimSuffix(configInterface, ".conf"), "wg-")
	c.logger.Info(fmt.Sprintf("loaded connection state from %s", configInterface))
	var exists bool
	exists = c.configGen.InterfaceExists(c.config.Interface)
	// in case the interface from state file is different from current config, and it exists, use it
	if !exists && c.configGen.InterfaceExists(configInterface) {
		c.logger.Info("Assigning interface from state file", "interface", configInterface)
		c.logger.Debug("Assigning interface from state file", "interface", configInterface)
		c.config.Interface = configInterface
	}

	if c.connected {
		c.logger.Info("restored connection state", "peer_id", c.peerID, "interface", c.config.Interface)
	}

	return nil
}

// getStateFile returns the path to the connection state file
func (c *Connector) getStateFile() string {
	keyDir := filepath.Dir(c.config.KeyPath)
	return filepath.Join(keyDir, fmt.Sprintf(".%s-state", c.config.Interface))
}

// Helper methods for config updates
func (c *Connector) updateClientIP(config, clientIP string) string {
	return c.configGen.UpdateConfigField(config, "Address", fmt.Sprintf("%s/32", clientIP))
}

func (c *Connector) updateDNS(config string, dnsServers []string) string {
	return c.configGen.UpdateConfigField(config, "DNS", joinStrings(dnsServers, ","))
}

func (c *Connector) updateAllowedIPs(config string, allowedIPs []string) string {
	return c.configGen.UpdateConfigField(config, "AllowedIPs", joinStrings(allowedIPs, ","))
}

// expandPath expands ~ to home directory in file paths
func expandPath(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if len(path) == 1 {
		return home
	}

	return filepath.Join(home, path[1:])
}

// joinStrings joins a slice of strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
