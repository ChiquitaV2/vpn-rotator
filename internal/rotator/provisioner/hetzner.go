package provisioner

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

//go:embed templates
var templatesFS embed.FS

// CloudInitData contains the data for rendering the cloud-init template.
type CloudInitData struct {
	ServerPrivateKey string
	ServerPublicKey  string
	SSHPublicKeys    []string
}

// ProvisionResult contains the result of a successful provisioning operation.
type ProvisionResult struct {
	ServerID        string // Hetzner server ID
	IPAddress       string // Public IPv4 address
	ServerPublicKey string // WireGuard server public key
	Port            int    // WireGuard port (51820)
}

// HetznerConfig contains configuration for the Hetzner provisioner.
type HetznerConfig struct {
	ServerType   string
	Image        string
	Location     string
	SSHPublicKey string // Path to SSH public key file or the key content itself
}

// Hetzner implements the Provisioner interface for Hetzner Cloud.
type Hetzner struct {
	client *hcloud.Client
	config *HetznerConfig
	logger *slog.Logger
}

// NewHetzner creates a new Hetzner provisioner.
func NewHetzner(apiToken string, config *HetznerConfig, logger *slog.Logger) (*Hetzner, error) {
	if apiToken == "" {
		return nil, fmt.Errorf("API token is required")
	}
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	client := hcloud.NewClient(hcloud.WithToken(apiToken))

	return &Hetzner{
		client: client,
		config: config,
		logger: logger,
	}, nil
}

// readSSHPublicKeys reads SSH public keys from the configured source.
func (h *Hetzner) readSSHPublicKeys() ([]string, error) {
	if h.config.SSHPublicKey == "" {
		h.logger.Warn("No SSH public key configured, SSH access will not be available")
		return []string{}, nil
	}

	var sshKeys []string

	// Check if it's a file path or direct key content
	if strings.HasPrefix(h.config.SSHPublicKey, "ssh-") || strings.HasPrefix(h.config.SSHPublicKey, "ecdsa-") || strings.HasPrefix(h.config.SSHPublicKey, "ed25519-") {
		// Direct key content
		sshKeys = append(sshKeys, strings.TrimSpace(h.config.SSHPublicKey))
	} else {
		// File path
		keyBytes, err := os.ReadFile(h.config.SSHPublicKey)
		if err != nil {
			return nil, &ProvisionError{
				Stage:     "ssh-key-read",
				Message:   "failed to read SSH public key file",
				Err:       err,
				Retryable: false,
			}
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
		h.logger.Warn("No valid SSH public keys found")
		return []string{}, nil
	}

	h.logger.Debug("Loaded SSH public keys", slog.Int("count", len(sshKeys)))
	return sshKeys, nil
}

// generateCloudInit renders the cloud-init template with WireGuard keys and SSH keys.
func (h *Hetzner) generateCloudInit(keys *KeyPair) (string, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/wireguard-server.yaml")
	if err != nil {
		return "", &ProvisionError{
			Stage:     "template-parse",
			Message:   "failed to parse cloud-init template",
			Err:       err,
			Retryable: false,
		}
	}

	// Read SSH public keys
	sshKeys, err := h.readSSHPublicKeys()
	if err != nil {
		return "", err // Already wrapped in ProvisionError
	}

	data := CloudInitData{
		ServerPrivateKey: keys.PrivateKey,
		ServerPublicKey:  keys.PublicKey,
		SSHPublicKeys:    sshKeys,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", &ProvisionError{
			Stage:     "template-render",
			Message:   "failed to render cloud-init template",
			Err:       err,
			Retryable: false,
		}
	}

	return buf.String(), nil
}

// healthCheck validates that the health server is reachable and WireGuard is running.
func (h *Hetzner) healthCheck(ctx context.Context, ipAddress string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Use the health endpoint on port 8080
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:8080/health", ipAddress), nil)
	if err != nil {
		return &ProvisionError{
			Stage:     "health-check",
			Message:   "failed to create health check request",
			Err:       err,
			Retryable: true,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return &ProvisionError{
			Stage:     "health-check",
			Message:   "health check request failed",
			Err:       err,
			Retryable: true,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ProvisionError{
			Stage:     "health-check",
			Message:   fmt.Sprintf("health check failed with status code: %d", resp.StatusCode),
			Err:       fmt.Errorf("HTTP %d", resp.StatusCode),
			Retryable: true,
		}
	}

	h.logger.Debug("Health check passed", slog.String("ip_address", ipAddress))
	return nil
}

// ProvisionNode creates a new VPN node with cloud-init setup.
func (h *Hetzner) ProvisionNode(ctx context.Context) (*models.Node, error) {
	// Step 1: Generate WireGuard keys
	h.logger.Info("Generating WireGuard key pair")
	keys, err := GenerateKeyPair()
	if err != nil {
		return nil, &ProvisionError{
			Stage:     "key-generation",
			Message:   "failed to generate WireGuard keys",
			Err:       err,
			Retryable: false,
		}
	}

	// Step 2: Render cloud-init template
	h.logger.Info("Rendering cloud-init configuration")
	cloudInit, err := h.generateCloudInit(keys)
	if err != nil {
		return nil, err // Already wrapped in ProvisionError
	}

	// step 3: get ssh keys
	allSSHKeys, err := h.client.SSHKey.All(ctx)

	// Step 3: Create server with cloud-init
	serverName := fmt.Sprintf("vpn-rotator-%d", time.Now().Unix())
	h.logger.Info("Creating Hetzner server",
		slog.String("server_name", serverName),
		slog.String("server_type", h.config.ServerType),
		slog.String("location", h.config.Location))

	serverCreateReq := hcloud.ServerCreateOpts{
		Name: serverName,
		ServerType: &hcloud.ServerType{
			Name: h.config.ServerType,
		},
		Image: &hcloud.Image{
			Name: h.config.Image,
		},
		Location: &hcloud.Location{
			Name: h.config.Location,
		},
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: true,
			EnableIPv6: false,
		},
		SSHKeys:  allSSHKeys,
		UserData: cloudInit,
	}

	result, _, err := h.client.Server.Create(ctx, serverCreateReq)
	if err != nil {
		return nil, &ProvisionError{
			Stage:     "server-create",
			Message:   "failed to create Hetzner server",
			Err:       err,
			Retryable: IsTransientError(err),
		}
	}

	serverID := fmt.Sprintf("%d", result.Server.ID)
	ipAddress := result.Server.PublicNet.IPv4.IP.String()

	h.logger.Info("Server created successfully",
		slog.String("server_id", serverID),
		slog.String("ip_address", ipAddress))

	// Step 4: Wait for cloud-init to complete (WireGuard setup + health server)
	h.logger.Info("Waiting for cloud-init to complete", slog.String("server_id", serverID))
	time.Sleep(90 * time.Second) // Allow more time for cloud-init to run (WireGuard + health server)

	// Step 5: Health check with retries
	const maxHealthChecks = 10
	const healthCheckInterval = 10 * time.Second

	var lastErr error
	for i := 0; i < maxHealthChecks; i++ {
		h.logger.Debug("Performing health check",
			slog.Int("attempt", i+1),
			slog.Int("max_attempts", maxHealthChecks))

		if err := h.healthCheck(ctx, ipAddress); err == nil {
			h.logger.Info("Node provisioned successfully",
				slog.String("server_id", serverID),
				slog.String("ip_address", ipAddress),
				slog.String("public_key", keys.PublicKey))

			return &models.Node{
				ID:        result.Server.ID, // Set the actual Hetzner server ID
				IP:        ipAddress,
				PublicKey: keys.PublicKey,
				Status:    models.NodeStatusActive,
			}, nil
		} else {
			lastErr = err
			h.logger.Warn("Health check failed, retrying",
				slog.Int("attempt", i+1),
				slog.String("error", err.Error()))
			time.Sleep(healthCheckInterval)
		}
	}

	// Health check failed - clean up the server
	h.logger.Error("Health checks exhausted, destroying server",
		slog.String("server_id", serverID))

	if destroyErr := h.DestroyNode(ctx, serverID); destroyErr != nil {
		h.logger.Error("Failed to clean up unhealthy server",
			slog.String("server_id", serverID),
			slog.String("error", destroyErr.Error()))
	}

	return nil, &ProvisionError{
		Stage:     "health-check",
		Message:   fmt.Sprintf("server failed health checks after %d attempts", maxHealthChecks),
		Err:       lastErr,
		Retryable: true,
		ServerID:  serverID,
	}
}

// DestroyNode deletes a VPN node.
func (h *Hetzner) DestroyNode(ctx context.Context, serverID string) error {
	h.logger.Info("Destroying node", slog.String("server_id", serverID))

	// Parse server ID
	var hetznerID int64
	if _, err := fmt.Sscanf(serverID, "%d", &hetznerID); err != nil {
		return &DestroyError{
			ServerID: serverID,
			Message:  "invalid server ID format",
			Err:      err,
		}
	}

	// Delete the server
	server := &hcloud.Server{ID: hetznerID}
	_, err := h.client.Server.Delete(ctx, server)
	if err != nil {
		return &DestroyError{
			ServerID: serverID,
			Message:  "failed to delete Hetzner server",
			Err:      err,
		}
	}

	h.logger.Info("Node destroyed successfully", slog.String("server_id", serverID))
	return nil
}
