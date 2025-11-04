package provisioner

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/pkg/crypto"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

//go:embed templates
var templatesFS embed.FS

// HetznerProvisioner implements CloudProvisioner for Hetzner Cloud
type HetznerProvisioner struct {
	client *hcloud.Client
	config *HetznerConfig
	logger *slog.Logger
}

// HetznerConfig contains configuration for the Hetzner provisioner
type HetznerConfig struct {
	ServerType   string `json:"server_type"`
	Image        string `json:"image"`
	Location     string `json:"location"`
	SSHPublicKey string `json:"ssh_public_key"` // Path to SSH public key file or the key content itself
}

// CloudInitData contains the data for rendering the cloud-init template
type CloudInitData struct {
	ServerPrivateKey string
	ServerPublicKey  string
	SSHPublicKeys    []string
	SubnetCIDR       string // e.g., "10.8.1.0/24"
	GatewayIP        string // e.g., "10.8.1.1"
}

// NewHetznerProvisioner creates a new Hetzner cloud provisioner
func NewHetznerProvisioner(apiToken string, config *HetznerConfig, logger *slog.Logger) (*HetznerProvisioner, error) {
	if apiToken == "" {
		return nil, fmt.Errorf("API token is required")
	}
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	client := hcloud.NewClient(hcloud.WithToken(apiToken))

	return &HetznerProvisioner{
		client: client,
		config: config,
		logger: logger,
	}, nil
}

// ProvisionNode provisions a new node with the given configuration
func (h *HetznerProvisioner) ProvisionNode(ctx context.Context, config node.ProvisioningConfig) (*node.ProvisionedNode, error) {
	h.logger.Info("starting node provisioning",
		slog.String("region", config.Region),
		slog.String("instance_type", config.InstanceType))

	// Step 1: Generate WireGuard keys
	h.logger.Debug("generating WireGuard key pair")
	keys, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, NewProvisioningError("", "key-generation", "failed to generate WireGuard keys", err, false)
	}

	// Step 2: Generate cloud-init configuration
	h.logger.Debug("generating cloud-init configuration")
	cloudInit, err := h.generateCloudInitWithSubnet(keys, config.Subnet, config.SSHPublicKey)
	if err != nil {
		return nil, err
	}

	// Step 3: Get SSH keys from Hetzner
	allSSHKeys, err := h.client.SSHKey.All(ctx)
	if err != nil {
		return nil, NewProvisioningError("", "ssh-keys", "failed to get SSH keys", err, true)
	}

	// Step 4: Create server
	serverName := fmt.Sprintf("vpn-rotator-%d", time.Now().Unix())
	h.logger.Info("creating Hetzner server",
		slog.String("server_name", serverName),
		slog.String("server_type", config.InstanceType),
		slog.String("location", config.Region))

	serverCreateReq := hcloud.ServerCreateOpts{
		Name: serverName,
		ServerType: &hcloud.ServerType{
			Name: config.InstanceType,
		},
		Image: &hcloud.Image{
			Name: config.ImageID,
		},
		Location: &hcloud.Location{
			Name: config.Region,
		},
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: true,
			EnableIPv6: false,
		},
		SSHKeys:  allSSHKeys,
		UserData: cloudInit,
		Labels:   config.Tags,
	}

	result, _, err := h.client.Server.Create(ctx, serverCreateReq)
	if err != nil {
		return nil, NewProvisioningError("", "server-create", "failed to create Hetzner server", err, h.isTransientError(err))
	}

	serverID := fmt.Sprintf("%d", result.Server.ID)
	ipAddress := result.Server.PublicNet.IPv4.IP.String()

	h.logger.Info("server created successfully",
		slog.String("server_id", serverID),
		slog.String("ip_address", ipAddress))

	// Step 5: Wait for cloud-init to complete
	h.logger.Info("waiting for cloud-init to complete", slog.String("server_id", serverID))
	time.Sleep(60 * time.Second) // Allow time for cloud-init to run

	// Step 6: Perform health checks
	if err := h.performHealthChecks(ctx, ipAddress); err != nil {
		// Clean up the server on health check failure
		h.logger.Error("health checks failed, cleaning up server", slog.String("server_id", serverID))
		if destroyErr := h.DestroyNode(ctx, serverID); destroyErr != nil {
			h.logger.Error("failed to clean up unhealthy server",
				slog.String("server_id", serverID),
				slog.String("error", destroyErr.Error()))
		}
		return nil, NewProvisioningError(serverID, "health-check", "server failed health checks", err, true)
	}

	h.logger.Info("node provisioned successfully",
		slog.String("server_id", serverID),
		slog.String("ip_address", ipAddress),
		slog.String("public_key", keys.PublicKey))

	return &node.ProvisionedNode{
		ServerID:  serverID,
		IPAddress: ipAddress,
		PublicKey: keys.PublicKey,
		Status:    "active",
		Region:    config.Region,
	}, nil
}

// DestroyNode destroys a node
func (h *HetznerProvisioner) DestroyNode(ctx context.Context, serverID string) error {
	h.logger.Info("destroying node", slog.String("server_id", serverID))

	// Parse server ID
	hetznerID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return NewDestructionError(serverID, "invalid server ID format", err)
	}

	// Delete the server
	server := &hcloud.Server{ID: hetznerID}
	_, err = h.client.Server.Delete(ctx, server)
	if err != nil {
		// Check if it's a "not found" error
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			h.logger.Warn("server not found, assuming already destroyed", slog.String("server_id", serverID))
			return nil
		}
		return NewDestructionError(serverID, "failed to delete Hetzner server", err)
	}

	h.logger.Info("node destroyed successfully", slog.String("server_id", serverID))
	return nil
}

// GetNodeStatus retrieves the status of a node
func (h *HetznerProvisioner) GetNodeStatus(ctx context.Context, serverID string) (*node.CloudNodeStatus, error) {
	h.logger.Debug("getting node status", slog.String("server_id", serverID))

	// Parse server ID
	hetznerID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid server ID format: %w", err)
	}

	// Get server from Hetzner
	server, _, err := h.client.Server.GetByID(ctx, hetznerID)
	if err != nil {
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			return &node.CloudNodeStatus{
				ServerID:  serverID,
				Status:    "not_found",
				IPAddress: "",
			}, nil
		}
		return nil, fmt.Errorf("failed to get server status: %w", err)
	}

	status := &node.CloudNodeStatus{
		ServerID: serverID,
		Status:   string(server.Status),
	}

	if server.PublicNet.IPv4.IP != nil {
		status.IPAddress = server.PublicNet.IPv4.IP.String()
	}

	return status, nil
}

// Private helper methods

func (h *HetznerProvisioner) generateCloudInitWithSubnet(keys *crypto.KeyPair, subnet *net.IPNet, sshPublicKey string) (string, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/wireguard-server.yaml")
	if err != nil {
		return "", NewProvisioningError("", "template-parse", "failed to parse cloud-init template", err, false)
	}

	// Read SSH public keys
	sshKeys, err := h.readSSHPublicKeys(sshPublicKey)
	if err != nil {
		return "", err
	}

	// Calculate subnet information
	var subnetCIDR, gatewayIP string
	if subnet != nil {
		subnetCIDR = subnet.String()
		// Calculate gateway IP (first IP in subnet)
		gatewayIPBytes := make(net.IP, len(subnet.IP))
		copy(gatewayIPBytes, subnet.IP)
		gatewayIPBytes[len(gatewayIPBytes)-1] = 1
		gatewayIP = gatewayIPBytes.String()
	} else {
		// Default subnet for backward compatibility
		subnetCIDR = "10.8.0.0/24"
		gatewayIP = "10.8.0.1"
	}

	data := CloudInitData{
		ServerPrivateKey: keys.PrivateKey,
		ServerPublicKey:  keys.PublicKey,
		SSHPublicKeys:    sshKeys,
		SubnetCIDR:       subnetCIDR,
		GatewayIP:        gatewayIP,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", NewProvisioningError("", "template-render", "failed to render cloud-init template", err, false)
	}

	return buf.String(), nil
}

func (h *HetznerProvisioner) readSSHPublicKeys(sshPublicKey string) ([]string, error) {
	// Use provided SSH key or fall back to config
	keySource := sshPublicKey
	if keySource == "" {
		keySource = h.config.SSHPublicKey
	}

	if keySource == "" {
		h.logger.Warn("no SSH public key configured, SSH access will not be available")
		return []string{}, nil
	}

	var sshKeys []string

	// Check if it's a file path or direct key content
	if strings.HasPrefix(keySource, "ssh-") || strings.HasPrefix(keySource, "ecdsa-") || strings.HasPrefix(keySource, "ed25519-") {
		// Direct key content
		sshKeys = append(sshKeys, strings.TrimSpace(keySource))
	} else {
		// File path
		keyBytes, err := os.ReadFile(keySource)
		if err != nil {
			return nil, NewProvisioningError("", "ssh-key-read", "failed to read SSH public key file", err, false)
		}

		// Split by lines in case there are multiple keys
		keyLines := strings.Split(string(keyBytes), "\n")
		for _, line := range keyLines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				sshKeys = append(sshKeys, line)
			}
		}
	}

	if len(sshKeys) == 0 {
		h.logger.Warn("no valid SSH public keys found")
		return []string{}, nil
	}

	h.logger.Debug("loaded SSH public keys", slog.Int("count", len(sshKeys)))
	return sshKeys, nil
}

func (h *HetznerProvisioner) performHealthChecks(ctx context.Context, ipAddress string) error {
	const maxHealthChecks = 10
	const healthCheckInterval = 10 * time.Second

	var lastErr error
	for i := 0; i < maxHealthChecks; i++ {
		h.logger.Debug("performing health check",
			slog.Int("attempt", i+1),
			slog.Int("max_attempts", maxHealthChecks))

		if err := h.healthCheck(ctx, ipAddress); err == nil {
			h.logger.Debug("health check passed", slog.String("ip_address", ipAddress))
			return nil
		} else {
			lastErr = err
			h.logger.Debug("health check failed, retrying",
				slog.Int("attempt", i+1),
				slog.String("error", err.Error()))

			if i < maxHealthChecks-1 {
				time.Sleep(healthCheckInterval)
			}
		}
	}

	return fmt.Errorf("server failed health checks after %d attempts: %w", maxHealthChecks, lastErr)
}

func (h *HetznerProvisioner) healthCheck(ctx context.Context, ipAddress string) error {
	// Check SSH connectivity
	if err := h.checkSSHConnectivity(ctx, ipAddress); err != nil {
		return fmt.Errorf("SSH connectivity check failed: %w", err)
	}

	// Check WireGuard port
	if err := h.checkWireGuardPort(ctx, ipAddress); err != nil {
		return fmt.Errorf("WireGuard port check failed: %w", err)
	}

	return nil
}

func (h *HetznerProvisioner) checkSSHConnectivity(ctx context.Context, ipAddress string) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:22", ipAddress), 10*time.Second)
	if err != nil {
		return fmt.Errorf("SSH port not accessible: %w", err)
	}
	conn.Close()
	return nil
}

func (h *HetznerProvisioner) checkWireGuardPort(ctx context.Context, ipAddress string) error {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:51820", ipAddress), 5*time.Second)
	if err != nil {
		return fmt.Errorf("WireGuard UDP port 51820 not accessible: %w", err)
	}
	conn.Close()
	return nil
}

func (h *HetznerProvisioner) isTransientError(err error) bool {
	// Check for common transient errors from Hetzner API
	if hcloud.IsError(err, hcloud.ErrorCodeRateLimitExceeded) {
		return true
	}
	if hcloud.IsError(err, hcloud.ErrorCodeResourceUnavailable) {
		return true
	}
	// Add more transient error checks as needed
	return false
}
