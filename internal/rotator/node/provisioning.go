package node

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
)

// ProvisioningService coordinates node provisioning across IP allocation, cloud provisioning, and node setup
type ProvisioningService struct {
	nodeService      NodeService
	repository       NodeRepository
	cloudProvisioner CloudProvisioner
	nodeInteractor   nodeinteractor.HealthChecker
	ipService        ip.Service
	progressReporter ProgressReporter
	logger           *slog.Logger
	config           ProvisioningServiceConfig
}

// ProgressReporter defines an interface for reporting provisioning progress
// This allows the service to report progress without coupling to specific event implementations
type ProgressReporter interface {
	ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error
	ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error
	ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error
	ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error
}

// ProvisioningServiceConfig contains configuration for node provisioning service
type ProvisioningServiceConfig struct {
	ProvisioningTimeout    time.Duration `json:"provisioning_timeout"`
	ReadinessTimeout       time.Duration `json:"readiness_timeout"`
	ReadinessCheckInterval time.Duration `json:"readiness_check_interval"`
	MaxRetryAttempts       int           `json:"max_retry_attempts"`
	RetryBackoff           time.Duration `json:"retry_backoff"`
	CleanupOnFailure       bool          `json:"cleanup_on_failure"`

	// Cloud provisioning defaults
	DefaultRegion       string            `json:"default_region"`
	DefaultInstanceType string            `json:"default_instance_type"`
	DefaultImageID      string            `json:"default_image_id"`
	DefaultSSHKey       string            `json:"default_ssh_key"`
	DefaultTags         map[string]string `json:"default_tags"`
}

// NewProvisioningService creates a new provisioning service
func NewProvisioningService(
	nodeService NodeService,
	repository NodeRepository,
	cloudProvisioner CloudProvisioner,
	nodeInteractor nodeinteractor.HealthChecker,
	ipService ip.Service,
	logger *slog.Logger,
	config ProvisioningServiceConfig,
) *ProvisioningService {
	return &ProvisioningService{
		nodeService:      nodeService,
		repository:       repository,
		cloudProvisioner: cloudProvisioner,
		nodeInteractor:   nodeInteractor,
		ipService:        ipService,
		progressReporter: nil,
		logger:           logger,
		config:           config,
	}
}

func (ps *ProvisioningService) SetProgressReporter(reporter ProgressReporter) {
	ps.progressReporter = reporter
}

// ProvisionNode provisions a new node with coordinated IP allocation, cloud provisioning, and setup
func (ps *ProvisioningService) ProvisionNode(ctx context.Context) (*Node, error) {
	startTime := time.Now()
	ps.logger.Info("starting coordinated node provisioning")

	// Create timeout context for the entire provisioning process
	provisioningCtx, cancel := context.WithTimeout(ctx, ps.config.ProvisioningTimeout)
	defer cancel()

	// Generate unique node ID
	nodeID := fmt.Sprintf("node-%d", time.Now().UnixNano())

	// Phase 1: Initialization (5%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "initialization", "Starting node provisioning")

	// Create database record immediately with "provisioning" status to prevent race conditions
	node, err := ps.createProvisioningRecord(provisioningCtx, nodeID)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "initialization", err.Error(), false)
		return nil, fmt.Errorf("failed to create provisioning record: %w", err)
	}

	ps.logger.Info("created provisioning node record", slog.String("node_id", nodeID))
	_ = ps.reportProgress(provisioningCtx, nodeID, "initialization", 0.05, "Database record created", nil)

	// Initialize rollback context for cleanup
	rollback := &provisioningRollback{
		nodeID:    nodeID,
		service:   ps,
		logger:    ps.logger,
		allocated: make(map[string]bool),
	}

	// Phase 2: Subnet Allocation (10%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "subnet_allocation", "Allocating IP subnet")

	subnet, err := ps.allocateSubnet(provisioningCtx, nodeID, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "subnet_allocation", err.Error(), true)
		ps.performRollback(provisioningCtx, rollback)
		return nil, fmt.Errorf("failed to allocate subnet: %w", err)
	}

	ps.logger.Info("allocated subnet for node",
		slog.String("node_id", nodeID),
		slog.String("subnet", subnet.String()))

	_ = ps.reportProgress(provisioningCtx, nodeID, "subnet_allocation", 0.10,
		fmt.Sprintf("Subnet allocated: %s", subnet.String()),
		map[string]interface{}{"subnet": subnet.String()})

	// Phase 3: Cloud Provisioning (65%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "cloud_provision", "Provisioning cloud infrastructure")

	provisionedNode, err := ps.provisionCloudInfrastructure(provisioningCtx, nodeID, subnet, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "cloud_provision", err.Error(), true)
		ps.performRollback(provisioningCtx, rollback)
		return nil, fmt.Errorf("failed to provision cloud infrastructure: %w", err)
	}

	ps.logger.Info("provisioned cloud infrastructure",
		slog.String("node_id", nodeID),
		slog.String("server_id", provisionedNode.ServerID),
		slog.String("ip", provisionedNode.IPAddress))

	_ = ps.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.65,
		fmt.Sprintf("Cloud server created: %s", provisionedNode.IPAddress),
		map[string]interface{}{
			"server_id":  provisionedNode.ServerID,
			"ip_address": provisionedNode.IPAddress,
		})

	// Phase 4: Update Database Details (70%)
	_ = ps.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.70, "Updating database with provisioned details", nil)

	err = ps.updateNodeDetails(provisioningCtx, node, provisionedNode, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "cloud_provision", err.Error(), false)
		ps.performRollback(provisioningCtx, rollback)
		return nil, fmt.Errorf("failed to update node details: %w", err)
	}

	// Phase 5: SSH Connection & Readiness (85%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "ssh_connection", "Waiting for SSH and node readiness")

	err = ps.waitForNodeReadiness(provisioningCtx, nodeID, provisionedNode.IPAddress, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "ssh_connection", err.Error(), true)
		ps.performRollback(provisioningCtx, rollback)
		return nil, fmt.Errorf("node readiness check failed: %w", err)
	}

	_ = ps.reportProgress(provisioningCtx, nodeID, "ssh_connection", 0.85, "Node is ready and accessible", nil)

	// Phase 6: Health Check (90%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "health_check", "Performing health checks")

	// Health check is already part of waitForNodeReadiness, so we just report progress
	_ = ps.reportProgress(provisioningCtx, nodeID, "health_check", 0.90, "Health checks passed", nil)

	// Phase 7: Activation (95%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "activation", "Activating node")

	err = ps.activateNode(provisioningCtx, node, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "activation", err.Error(), false)
		ps.performRollback(provisioningCtx, rollback)
		return nil, fmt.Errorf("failed to activate node: %w", err)
	}

	_ = ps.reportProgress(provisioningCtx, nodeID, "activation", 0.95, "Node activated successfully", nil)

	// Phase 8: Completion (100%)
	duration := time.Since(startTime)
	ps.logger.Info("node provisioning completed successfully",
		slog.String("node_id", nodeID),
		slog.String("ip", provisionedNode.IPAddress),
		slog.String("public_key", provisionedNode.PublicKey),
		slog.Duration("duration", duration))

	_ = ps.reportProgress(provisioningCtx, nodeID, "completed", 1.0, "Node provisioning completed",
		map[string]interface{}{
			"server_id":        provisionedNode.ServerID,
			"ip_address":       provisionedNode.IPAddress,
			"public_key":       provisionedNode.PublicKey,
			"duration_seconds": duration.Seconds(),
		})

	// Return the final node state
	finalNode, err := ps.repository.GetByID(provisioningCtx, nodeID)
	if err != nil {
		ps.logger.Warn("failed to get final node state",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return node, nil // Return the node we have
	}

	return finalNode, nil
}

// DestroyNode destroys a node with coordinated cleanup
func (ps *ProvisioningService) DestroyNode(ctx context.Context, nodeID string) error {
	ps.logger.Info("starting coordinated node destruction", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := ps.repository.GetByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	// Validate node can be destroyed
	if err := node.ValidateForDestruction(); err != nil {
		return fmt.Errorf("node validation failed: %w", err)
	}

	// Update status to destroying to prevent race conditions
	if err := node.UpdateStatus(StatusDestroying); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	if err := ps.repository.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	// Step 1: Release subnet allocation
	err = ps.ipService.ReleaseNodeSubnet(ctx, nodeID)
	if err != nil {
		ps.logger.Warn("failed to release node subnet",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		// Continue with destruction even if subnet cleanup fails
	}

	// Step 2: Destroy cloud infrastructure
	if node.ServerID != "" {
		err = ps.cloudProvisioner.DestroyNode(ctx, node.ServerID)
		if err != nil {
			ps.logger.Error("failed to destroy cloud infrastructure",
				slog.String("node_id", nodeID),
				slog.String("server_id", node.ServerID),
				slog.String("error", err.Error()))
			// Continue with database cleanup even if cloud destruction fails
		}
	}

	// Step 3: Remove from database
	err = ps.repository.Delete(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to delete node from database: %w", err)
	}

	ps.logger.Info("node destruction completed successfully", slog.String("node_id", nodeID))
	return nil
}

// Progress reporting helper methods

func (ps *ProvisioningService) reportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	if ps.progressReporter == nil {
		return nil // Silently skip if no reporter configured
	}
	return ps.progressReporter.ReportProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (ps *ProvisioningService) reportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	if ps.progressReporter == nil {
		return nil
	}
	return ps.progressReporter.ReportPhaseStart(ctx, nodeID, phase, message)
}

func (ps *ProvisioningService) reportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	if ps.progressReporter == nil {
		return nil
	}
	return ps.progressReporter.ReportPhaseComplete(ctx, nodeID, phase, message)
}

func (ps *ProvisioningService) reportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	if ps.progressReporter == nil {
		return nil
	}
	return ps.progressReporter.ReportError(ctx, nodeID, phase, errorMsg, retryable)
}

// Helper methods for provisioning steps

func (ps *ProvisioningService) createProvisioningRecord(ctx context.Context, nodeID string) (*Node, error) {
	// Create node with provisioning status
	node, err := NewNode(nodeID, "0.0.0.0", nodeID /*Must be unique at start and will be updated after provisioning*/, 51820) // Placeholder values
	if err != nil {
		return nil, err
	}

	// Save to repository
	if err := ps.repository.Create(ctx, node); err != nil {
		slog.Debug("failed to create provisioning record", slog.String("node_id", nodeID), slog.String("error", err.Error()))
		return nil, err
	}

	return node, nil
}

func (ps *ProvisioningService) allocateSubnet(ctx context.Context, nodeID string, rollback *provisioningRollback) (*net.IPNet, error) {
	subnet, err := ps.ipService.AllocateNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	rollback.allocated["subnet"] = true
	return subnet, nil
}

func (ps *ProvisioningService) provisionCloudInfrastructure(ctx context.Context, nodeID string, subnet *net.IPNet, rollback *provisioningRollback) (*ProvisionedNode, error) {
	// Report sub-phase: preparing configuration
	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.15, "Preparing cloud configuration", nil)

	// Create provisioning config using service defaults
	config := ProvisioningConfig{
		Region:       ps.getConfigValue(ps.config.DefaultRegion, "nbg1"),
		InstanceType: ps.getConfigValue(ps.config.DefaultInstanceType, "cx11"),
		ImageID:      ps.getConfigValue(ps.config.DefaultImageID, "ubuntu-22.04"),
		SSHKeyName:   "",
		Tags:         ps.getDefaultTags(),
		Subnet:       subnet,
		SSHPublicKey: ps.config.DefaultSSHKey,
	}

	// Report sub-phase: calling cloud provisioner
	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.20, "Creating cloud server", nil)

	// Use the CloudProvisioner interface
	provisionedNode, err := ps.cloudProvisioner.ProvisionNode(ctx, config)
	if err != nil {
		return nil, err
	}

	rollback.allocated["cloud"] = true
	rollback.serverID = provisionedNode.ServerID

	// Report sub-phase: waiting for cloud-init
	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.40, "Waiting for cloud-init to complete",
		map[string]interface{}{
			"server_id":  provisionedNode.ServerID,
			"ip_address": provisionedNode.IPAddress,
		})

	// Wait for cloud-init (this is a significant portion of provisioning time)
	time.Sleep(60 * time.Second)

	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.60, "Cloud-init completed", nil)

	return provisionedNode, nil
}

// Helper methods for configuration

func (ps *ProvisioningService) getConfigValue(configValue, defaultValue string) string {
	if configValue != "" {
		return configValue
	}
	return defaultValue
}

func (ps *ProvisioningService) getDefaultTags() map[string]string {
	tags := map[string]string{
		"service": "vpn-rotator",
		"type":    "vpn-node",
	}

	// Merge with configured default tags
	for k, v := range ps.config.DefaultTags {
		tags[k] = v
	}

	return tags
}

func (ps *ProvisioningService) updateNodeDetails(ctx context.Context, node *Node, provisionedNode *ProvisionedNode, rollback *provisioningRollback) error {
	// Update node with provisioned details
	node.ServerID = provisionedNode.ServerID
	node.IPAddress = provisionedNode.IPAddress
	node.ServerPublicKey = provisionedNode.PublicKey
	node.UpdatedAt = time.Now()
	node.Version++

	// Save to repository
	if err := ps.repository.Update(ctx, node); err != nil {
		slog.Debug("failed to update node details", slog.String("node_id", node.ID), slog.String("error", err.Error()))
		return err
	}

	rollback.allocated["details"] = true
	return nil
}

func (ps *ProvisioningService) waitForNodeReadiness(ctx context.Context, nodeID, nodeIP string, rollback *provisioningRollback) error {
	readinessCtx, cancel := context.WithTimeout(ctx, ps.config.ReadinessTimeout)
	defer cancel()

	ticker := time.NewTicker(ps.config.ReadinessCheckInterval)
	defer ticker.Stop()

	attemptCount := 0
	for {
		select {
		case <-readinessCtx.Done():
			return fmt.Errorf("timed out waiting for node %s to be ready", nodeIP)
		case <-ticker.C:
			attemptCount++
			ps.logger.Debug("checking node readiness",
				slog.String("node_id", nodeID),
				slog.String("ip", nodeIP),
				slog.Int("attempt", attemptCount))

			// Report progress during readiness checks
			_ = ps.reportProgress(ctx, nodeID, "ssh_connection", 0.75,
				fmt.Sprintf("Checking node readiness (attempt %d)", attemptCount), nil)

			// Check node health using NodeInteractor
			health, err := ps.nodeInteractor.CheckNodeHealth(readinessCtx, nodeIP)
			if err == nil && health.IsHealthy {
				ps.logger.Info("node is ready",
					slog.String("node_id", nodeID),
					slog.String("ip", nodeIP))
				return nil
			}

			if err != nil {
				ps.logger.Debug("node not ready yet",
					slog.String("node_id", nodeID),
					slog.String("ip", nodeIP),
					slog.String("error", err.Error()))
			}
		}
	}
}

func (ps *ProvisioningService) activateNode(ctx context.Context, node *Node, rollback *provisioningRollback) error {
	// Update status to active
	if err := node.UpdateStatus(StatusActive); err != nil {
		return err
	}

	// Save to repository
	if err := ps.repository.Update(ctx, node); err != nil {
		return err
	}

	rollback.allocated["active"] = true
	return nil
}

func (ps *ProvisioningService) performRollback(ctx context.Context, rollback *provisioningRollback) {
	if !ps.config.CleanupOnFailure {
		ps.logger.Info("cleanup on failure disabled, skipping rollback")
		return
	}

	ps.logger.Info("performing provisioning rollback", slog.String("node_id", rollback.nodeID))
	rollback.execute(ctx)
}

// provisioningRollback handles cleanup operations for failed provisioning
type provisioningRollback struct {
	nodeID    string
	serverID  string
	service   *ProvisioningService
	logger    *slog.Logger
	allocated map[string]bool
}

// execute performs the rollback operations in reverse order
func (r *provisioningRollback) execute(ctx context.Context) {
	// Rollback in reverse order of allocation

	// Remove active status (if set)
	if r.allocated["active"] {
		r.logger.Debug("rolling back node activation", slog.String("node_id", r.nodeID))
		// Status rollback is handled by deleting the node
	}

	// Remove updated details (if set)
	if r.allocated["details"] {
		r.logger.Debug("rolling back node details update", slog.String("node_id", r.nodeID))
		// Details rollback is handled by deleting the node
	}

	// Destroy cloud infrastructure (if provisioned)
	if r.allocated["cloud"] && r.serverID != "" {
		r.logger.Debug("rolling back cloud infrastructure",
			slog.String("node_id", r.nodeID),
			slog.String("server_id", r.serverID))

		if err := r.service.cloudProvisioner.DestroyNode(ctx, r.serverID); err != nil {
			r.logger.Error("failed to rollback cloud infrastructure",
				slog.String("node_id", r.nodeID),
				slog.String("server_id", r.serverID),
				slog.String("error", err.Error()))
		}
	}

	// Release subnet (if allocated)
	if r.allocated["subnet"] {
		r.logger.Debug("rolling back subnet allocation", slog.String("node_id", r.nodeID))

		if err := r.service.ipService.ReleaseNodeSubnet(ctx, r.nodeID); err != nil {
			r.logger.Error("failed to rollback subnet allocation",
				slog.String("node_id", r.nodeID),
				slog.String("error", err.Error()))
		}
	}

	// Delete database record (always attempt this)
	r.logger.Debug("rolling back database record", slog.String("node_id", r.nodeID))

	if err := r.service.repository.Delete(ctx, r.nodeID); err != nil {
		r.logger.Error("failed to rollback database record",
			slog.String("node_id", r.nodeID),
			slog.String("error", err.Error()))
	}

	r.logger.Info("provisioning rollback completed", slog.String("node_id", r.nodeID))
}
