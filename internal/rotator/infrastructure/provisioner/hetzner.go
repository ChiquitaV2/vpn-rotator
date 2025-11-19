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
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

//go:embed templates
var templatesFS embed.FS

// HetznerProvisioner implements CloudProvisioner for Hetzner Cloud
type HetznerProvisioner struct {
	client *hcloud.Client
	config *HetznerConfig
	logger *applogger.Logger // <-- Changed
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
func NewHetznerProvisioner(apiToken string, config *HetznerConfig, logger *applogger.Logger) (*HetznerProvisioner, error) { // <-- Changed
	if apiToken == "" {
		return nil, apperrors.NewSystemError(apperrors.ErrCodeConfiguration, "Hetzner API token is required", false, nil)
	}
	if config == nil {
		return nil, apperrors.NewSystemError(apperrors.ErrCodeConfiguration, "Hetzner config is required", false, nil)
	}

	client := hcloud.NewClient(hcloud.WithToken(apiToken))

	return &HetznerProvisioner{
		client: client,
		config: config,
		logger: logger.WithComponent("hetzner.provisioner"), // <-- Scoped logger
	}, nil
}

// ProvisionNode provisions a new node with the given configuration
func (h *HetznerProvisioner) ProvisionNode(ctx context.Context, config node.ProvisioningConfig) (*node.ProvisionedNode, error) {
	op := h.logger.StartOp(ctx, "ProvisionNode", slog.String("region", config.Region), slog.String("type", config.InstanceType))

	// Step 1: Generate WireGuard keys
	keys, err := crypto.GenerateKeyPair()
	if err != nil {
		err = apperrors.NewProvisioningError(apperrors.ErrCodeInternal, "failed to generate WireGuard keys", false, err)
		op.Fail(err, "key generation failed")
		return nil, err
	}
	op.Progress("keys generated")

	// Step 2: Generate cloud-init configuration
	cloudInit, err := h.generateCloudInitWithSubnet(ctx, keys, config.Subnet, config.SSHPublicKey)
	if err != nil { // Already a DomainError
		op.Fail(err, "cloud-init generation failed")
		return nil, err
	}
	op.Progress("cloud-init generated")

	// Step 3: Get SSH keys from Hetzner
	allSSHKeys, err := h.client.SSHKey.All(ctx)
	if err != nil {
		err = apperrors.NewProvisioningError(apperrors.ErrCodeProvisionFailed, "failed to get SSH keys from Hetzner", true, err)
		op.Fail(err, "failed to get ssh keys")
		return nil, err
	}
	op.Progress("ssh keys retrieved")

	// Step 4: Create server
	serverName := fmt.Sprintf("vpn-rotator-%d", time.Now().UnixNano())
	serverCreateReq := hcloud.ServerCreateOpts{
		Name:       serverName,
		ServerType: &hcloud.ServerType{Name: config.InstanceType},
		Image:      &hcloud.Image{Name: config.ImageID},
		Location:   &hcloud.Location{Name: config.Region},
		PublicNet:  &hcloud.ServerCreatePublicNet{EnableIPv4: true, EnableIPv6: false},
		SSHKeys:    allSSHKeys,
		UserData:   cloudInit,
		Labels:     config.Tags,
	}

	result, _, err := h.client.Server.Create(ctx, serverCreateReq)
	if err != nil {
		err = apperrors.NewProvisioningError(apperrors.ErrCodeProvisionFailed, "failed to create Hetzner server", h.isTransientError(err), err)
		op.Fail(err, "failed to create server")
		return nil, err
	}
	op.Progress("server created")

	serverID := fmt.Sprintf("%d", result.Server.ID)
	ipAddress := result.Server.PublicNet.IPv4.IP.String()
	op.With(slog.String("server_id", serverID), slog.String("ip_address", ipAddress))

	// Step 5: Wait for cloud-init to complete
	op.Progress("waiting for cloud-init")
	time.Sleep(60 * time.Second)

	// Step 6: Perform health checks
	if err := h.performHealthChecks(ctx, ipAddress); err != nil {
		err = apperrors.NewProvisioningError(apperrors.ErrCodeHealthCheckFailed, "server failed health checks", true, err)
		op.Fail(err, "health checks failed")

		// Attempt to clean up the failed server
		if destroyErr := h.DestroyNode(ctx, serverID); destroyErr != nil {
			h.logger.ErrorCtx(ctx, "failed to clean up unhealthy server", destroyErr, slog.String("server_id", serverID))
		}
		return nil, err
	}
	op.Progress("health checks passed")

	op.Complete("node provisioned successfully")
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
	op := h.logger.StartOp(ctx, "DestroyNode", slog.String("server_id", serverID))

	hetznerID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeValidation, "invalid server ID format", false, err)
		op.Fail(err, "invalid server id")
		return err
	}

	server := &hcloud.Server{ID: hetznerID}
	_, err = h.client.Server.Delete(ctx, server)
	if err != nil {
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			op.Complete("server not found, assuming already destroyed")
			return nil
		}
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeDestructionFailed, "failed to delete Hetzner server", true, err)
		op.Fail(err, "failed to delete server")
		return err
	}

	op.Complete("node destroyed successfully")
	return nil
}

// GetNodeStatus retrieves the status of a node
func (h *HetznerProvisioner) GetNodeStatus(ctx context.Context, serverID string) (*node.CloudNodeStatus, error) {
	op := h.logger.StartOp(ctx, "GetNodeStatus", slog.String("server_id", serverID))

	hetznerID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeValidation, "invalid server ID format", false, err)
		op.Fail(err, "invalid server id")
		return nil, err
	}

	server, _, err := h.client.Server.GetByID(ctx, hetznerID)
	if err != nil {
		if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			op.Complete("server not found")
			return &node.CloudNodeStatus{ServerID: serverID, Status: "not_found"}, nil
		}
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeNetworkError, "failed to get server status", true, err)
		op.Fail(err, "failed to get server status")
		return nil, err
	}

	status := &node.CloudNodeStatus{
		ServerID: serverID,
		Status:   string(server.Status),
	}
	if server.PublicNet.IPv4.IP != nil {
		status.IPAddress = server.PublicNet.IPv4.IP.String()
	}

	op.Complete("status retrieved", slog.String("status", status.Status))
	return status, nil
}

// Private helper methods

func (h *HetznerProvisioner) generateCloudInitWithSubnet(ctx context.Context, keys *crypto.KeyPair, subnet *net.IPNet, sshPublicKey string) (string, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/wireguard-server.yaml")
	if err != nil {
		return "", apperrors.NewProvisioningError(apperrors.ErrCodeConfiguration, "failed to parse cloud-init template", false, err)
	}

	sshKeys, err := h.readSSHPublicKeys(ctx, sshPublicKey)
	if err != nil {
		return "", err
	}

	var subnetCIDR, gatewayIP string
	if subnet != nil {
		subnetCIDR = subnet.String()
		gatewayIPBytes := make(net.IP, len(subnet.IP))
		copy(gatewayIPBytes, subnet.IP)
		gatewayIPBytes[len(gatewayIPBytes)-1] = 1
		gatewayIP = gatewayIPBytes.String()
	} else {
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
		return "", apperrors.NewProvisioningError(apperrors.ErrCodeConfiguration, "failed to render cloud-init template", false, err)
	}

	return buf.String(), nil
}

func (h *HetznerProvisioner) readSSHPublicKeys(ctx context.Context, sshPublicKey string) ([]string, error) {
	keySource := sshPublicKey
	if keySource == "" {
		h.logger.WarnContext(ctx, "no SSH public key configured, SSH access will not be available")
		return []string{}, nil
	}

	var sshKeys []string
	if strings.HasPrefix(keySource, "ssh-") || strings.HasPrefix(keySource, "ecdsa-") || strings.HasPrefix(keySource, "ed25519-") {
		sshKeys = append(sshKeys, strings.TrimSpace(keySource))
	} else {
		keyBytes, err := os.ReadFile(keySource)
		if err != nil {
			return nil, apperrors.NewProvisioningError(apperrors.ErrCodeConfiguration, "failed to read SSH public key file", false, err).
				WithMetadata("path", keySource)
		}
		keyLines := strings.Split(string(keyBytes), "\n")
		for _, line := range keyLines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				sshKeys = append(sshKeys, line)
			}
		}
	}

	h.logger.DebugContext(ctx, "loaded SSH public keys", slog.Int("count", len(sshKeys)))
	return sshKeys, nil
}

func (h *HetznerProvisioner) performHealthChecks(ctx context.Context, ipAddress string) error {
	const maxHealthChecks = 10
	const healthCheckInterval = 10 * time.Second

	var lastErr error
	for i := 0; i < maxHealthChecks; i++ {
		h.logger.DebugContext(ctx, "performing health check", "attempt", i+1, "max_attempts", maxHealthChecks, "ip_address", ipAddress)
		if err := h.healthCheck(ctx, ipAddress); err == nil {
			h.logger.DebugContext(ctx, "health check passed", "ip_address", ipAddress)
			return nil
		} else {
			lastErr = err
			h.logger.DebugContext(ctx, "health check failed, retrying", "attempt", i+1, "error", err.Error())
			if i < maxHealthChecks-1 {
				time.Sleep(healthCheckInterval)
			}
		}
	}

	return apperrors.NewProvisioningError(apperrors.ErrCodeHealthCheckFailed,
		fmt.Sprintf("server failed health checks after %d attempts", maxHealthChecks), true, lastErr).
		WithMetadata("ip_address", ipAddress)
}

func (h *HetznerProvisioner) healthCheck(ctx context.Context, ipAddress string) error {
	if err := h.checkSSHConnectivity(ctx, ipAddress); err != nil {
		return err
	}
	if err := h.checkWireGuardPort(ctx, ipAddress); err != nil {
		return err
	}
	return nil
}

func (h *HetznerProvisioner) checkSSHConnectivity(ctx context.Context, ipAddress string) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:22", ipAddress), 10*time.Second)
	if err != nil {
		return apperrors.NewProvisioningError(apperrors.ErrCodeHealthCheckFailed, "SSH port not accessible", true, err).WithMetadata("ip_address", ipAddress)
	}
	conn.Close()
	return nil
}

func (h *HetznerProvisioner) checkWireGuardPort(ctx context.Context, ipAddress string) error {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:51820", ipAddress), 5*time.Second)
	if err != nil {
		return apperrors.NewProvisioningError(apperrors.ErrCodeHealthCheckFailed, "WireGuard UDP port 51820 not accessible", true, err).WithMetadata("ip_address", ipAddress)
	}
	conn.Close()
	return nil
}

func (h *HetznerProvisioner) isTransientError(err error) bool {
	if hcloud.IsError(err, hcloud.ErrorCodeRateLimitExceeded) {
		return true
	}
	if hcloud.IsError(err, hcloud.ErrorCodeResourceUnavailable) {
		return true
	}
	return false
}
