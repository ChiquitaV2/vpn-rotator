package node

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/remote"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// ProvisioningService coordinates node provisioning across IP allocation, cloud provisioning, and node setup
type ProvisioningService struct {
	nodeService      NodeService
	repository       NodeRepository
	cloudProvisioner CloudProvisioner
	nodeInteractor   remote.HealthChecker
	ipService        ip.Service
	progressReporter ProgressReporter
	logger           *applogger.Logger // <-- Changed
	config           ProvisioningServiceConfig
}

// ProgressReporter defines an interface for reporting provisioning progress
type ProgressReporter interface {
	ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error
	ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error
	ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error
	ReportError(ctx context.Context, nodeID, phase string, err error) error // <-- Signature Changed
	ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error
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
	nodeInteractor remote.HealthChecker,
	ipService ip.Service,
	logger *applogger.Logger, // <-- Changed
	config ProvisioningServiceConfig,
) *ProvisioningService {
	return &ProvisioningService{
		nodeService:      nodeService,
		repository:       repository,
		cloudProvisioner: cloudProvisioner,
		nodeInteractor:   nodeInteractor,
		ipService:        ipService,
		progressReporter: nil,                                          // Must be set via SetProgressReporter
		logger:           logger.WithComponent("provisioning.service"), // <-- Scoped logger
		config:           config,
	}
}

func (ps *ProvisioningService) SetProgressReporter(reporter ProgressReporter) {
	ps.progressReporter = reporter
}

// ProvisionNode provisions a new node with coordinated IP allocation, cloud provisioning, and setup
func (ps *ProvisioningService) ProvisionNode(ctx context.Context) (*Node, error) {
	op := ps.logger.StartOp(ctx, "ProvisionNode")

	// Create timeout context for the entire provisioning process
	provisioningCtx, cancel := context.WithTimeout(ctx, ps.config.ProvisioningTimeout)
	defer cancel()

	// Generate unique node ID
	nodeID := fmt.Sprintf("node-%d", time.Now().UnixNano())

	// Phase 1: Initialization (5%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "initialization", "Starting node provisioning")

	// Create database record immediately
	node, err := ps.createProvisioningRecord(provisioningCtx, nodeID)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "initialization", err)
		ps.logger.ErrorCtx(ctx, "failed to create provisioning record", err)
		op.Fail(err, "failed to create provisioning record")
		return nil, err
	}

	ps.logger.DebugContext(ctx, "created provisioning node record", slog.String("node_id", nodeID))
	_ = ps.reportProgress(provisioningCtx, nodeID, "initialization", 0.05, "Database record created", nil)

	// Initialize rollback context for cleanup
	rollback := &provisioningRollback{
		nodeID:    nodeID,
		service:   ps,
		logger:    ps.logger, // Pass the logger
		allocated: make(map[string]bool),
	}

	// Phase 2: Subnet Allocation (10%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "subnet_allocation", "Allocating IP subnet")

	subnet, err := ps.allocateSubnet(provisioningCtx, nodeID, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "subnet_allocation", err)
		ps.performRollback(provisioningCtx, rollback)
		ps.logger.ErrorCtx(ctx, "failed to allocate subnet", err)
		op.Fail(err, "failed to allocate subnet")
		return nil, err
	}
	_ = ps.reportProgress(provisioningCtx, nodeID, "subnet_allocation", 0.10, "Subnet allocated", nil)

	// Phase 3: Cloud Provisioning (65%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "cloud_provision", "Provisioning cloud infrastructure")

	provisionedNode, err := ps.provisionCloudInfrastructure(provisioningCtx, nodeID, subnet, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "cloud_provision", err)
		ps.performRollback(provisioningCtx, rollback)
		ps.logger.ErrorCtx(ctx, "failed to provision cloud infrastructure", err)
		op.Fail(err, "failed to provision cloud infrastructure")
		return nil, err
	}
	_ = ps.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.65, "Cloud server created", nil)

	// Phase 4: Update Database Details (70%)
	_ = ps.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.70, "Updating database with provisioned details", nil)

	err = ps.updateNodeDetails(provisioningCtx, node, provisionedNode, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "cloud_provision", err)
		ps.performRollback(provisioningCtx, rollback)
		ps.logger.ErrorCtx(ctx, "failed to update node details", err)
		op.Fail(err, "failed to update node details")
		return nil, err
	}

	// Phase 5: SSH Connection & Readiness (85%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "ssh_connection", "Waiting for SSH and node readiness")

	err = ps.waitForNodeReadiness(provisioningCtx, nodeID, provisionedNode.IPAddress, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "ssh_connection", err)
		ps.performRollback(provisioningCtx, rollback)
		ps.logger.ErrorCtx(ctx, "node readiness check failed", err)
		op.Fail(err, "node readiness check failed")
		return nil, err
	}
	_ = ps.reportProgress(provisioningCtx, nodeID, "ssh_connection", 0.85, "Node is ready and accessible", nil)

	// Phase 6: Health Check (90%)
	// ... (This is part of readiness)
	_ = ps.reportProgress(provisioningCtx, nodeID, "health_check", 0.90, "Health checks passed", nil)

	// Phase 7: Activation (95%)
	_ = ps.reportPhaseStart(provisioningCtx, nodeID, "activation", "Activating node")

	err = ps.activateNode(provisioningCtx, node, rollback)
	if err != nil {
		_ = ps.reportError(provisioningCtx, nodeID, "activation", err)
		ps.performRollback(provisioningCtx, rollback)
		ps.logger.ErrorCtx(ctx, "failed to activate node", err)
		op.Fail(err, "failed to activate node")
		return nil, err
	}
	_ = ps.reportProgress(provisioningCtx, nodeID, "activation", 0.95, "Node activated successfully", nil)

	// Phase 8: Completion (100%)
	duration := time.Since(op.StartTime)
	_ = ps.reportProgress(provisioningCtx, nodeID, "completed", 1.0, "Node provisioning completed",
		map[string]interface{}{"duration_seconds": duration.Seconds()})

	// Report to event bus
	if ps.progressReporter != nil {
		_ = ps.progressReporter.ReportCompleted(ctx, nodeID, provisionedNode.ServerID, provisionedNode.IPAddress, duration)
	}

	// Return the final node state
	finalNode, err := ps.repository.GetByID(provisioningCtx, nodeID)
	if err != nil {
		ps.logger.ErrorCtx(ctx, "failed to get final node state, returning in-memory node", err,
			slog.String("node_id", nodeID))
		op.Complete("node provisioning completed with in-memory node returned")
		return node, nil // Return the node we have
	}

	op.Complete("node provisioning completed", slog.String("node_id", nodeID))
	return finalNode, nil
}

// DestroyNode destroys a node with coordinated cleanup
func (ps *ProvisioningService) DestroyNode(ctx context.Context, nodeID string) error {
	op := ps.logger.StartOp(ctx, "DestroyNode", slog.String("node_id", nodeID))

	// Get node from repository
	node, err := ps.repository.GetByID(ctx, nodeID)
	if err != nil {
		ps.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return err // Repo returns DomainError
	}

	// Validate node can be destroyed
	if err := node.ValidateForDestruction(); err != nil {
		ps.logger.ErrorCtx(ctx, "node validation for destruction failed", err)
		op.Fail(err, "node validation for destruction failed")
		return err // Model returns DomainError
	}

	// Update status to destroying
	if err := node.UpdateStatus(StatusDestroying); err != nil {
		ps.logger.ErrorCtx(ctx, "failed to update node status to destroying", err)
		op.Fail(err, "failed to update node status to destroying")
		return err // Model returns DomainError
	}

	if err := ps.repository.Update(ctx, node); err != nil {
		ps.logger.ErrorCtx(ctx, "failed to save destroying status", err)
		op.Fail(err, "failed to save destroying status")
		return err // Repo returns DomainError
	}

	// Step 1: Release subnet allocation
	if err = ps.ipService.ReleaseNodeSubnet(ctx, nodeID); err != nil {
		ps.logger.ErrorCtx(ctx, "failed to release node subnet, continuing destruction", err,
			slog.String("node_id", nodeID))
		// Continue with destruction
	}

	// Step 2: Destroy cloud infrastructure
	if node.ServerID != "" {
		if err = ps.cloudProvisioner.DestroyNode(ctx, node.ServerID); err != nil {
			ps.logger.ErrorCtx(ctx, "failed to destroy cloud infrastructure, continuing with db delete", err,
				slog.String("server_id", node.ServerID))
			// Continue with database cleanup
		}
	}

	// Step 3: Remove from database
	if err = ps.repository.Delete(ctx, nodeID); err != nil {
		ps.logger.ErrorCtx(ctx, "failed to delete node from database", err)
		op.Fail(err, "failed to delete node from database")
		return err
	}

	op.Complete("node destroyed successfully")
	return nil
}

// Progress reporting helper methods

func (ps *ProvisioningService) reportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	if ps.progressReporter == nil {
		return nil // Silently skip
	}
	return ps.progressReporter.ReportProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (ps *ProvisioningService) reportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	if ps.progressReporter == nil {
		return nil
	}
	return ps.progressReporter.ReportPhaseStart(ctx, nodeID, phase, message)
}

func (ps *ProvisioningService) reportError(ctx context.Context, nodeID, phase string, err error) error {
	if ps.progressReporter == nil {
		return nil
	}
	return ps.progressReporter.ReportError(ctx, nodeID, phase, err) // Pass the full error
}

// Helper methods for provisioning steps

func (ps *ProvisioningService) createProvisioningRecord(ctx context.Context, nodeID string) (*Node, error) {
	node, err := NewNode(nodeID, "0.0.0.0", nodeID, 51820) // Placeholder values
	if err != nil {
		return nil, err // Will be a DomainError from NewNode
	}

	// Save to repository
	if err := ps.repository.Create(ctx, node); err != nil {
		ps.logger.DebugContext(ctx, "failed to create provisioning record", slog.String("error", err.Error()))
		return nil, err // Repo returns DomainError
	}
	return node, nil
}

func (ps *ProvisioningService) allocateSubnet(ctx context.Context, nodeID string, rollback *provisioningRollback) (*net.IPNet, error) {
	subnet, err := ps.ipService.AllocateNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, err // ipService should return a DomainError
	}
	rollback.allocated["subnet"] = true
	return subnet, nil
}

func (ps *ProvisioningService) provisionCloudInfrastructure(ctx context.Context, nodeID string, subnet *net.IPNet, rollback *provisioningRollback) (*ProvisionedNode, error) {
	// Report sub-phase: preparing configuration
	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.15, "Preparing cloud configuration", nil)

	// Create provisioning config - location and instance type will be determined by the provisioner
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

	provisionedNode, err := ps.cloudProvisioner.ProvisionNode(ctx, config)
	if err != nil {
		// Assume cloudProvisioner returns a DomainError or wrap it
		return nil, apperrors.WrapWithDomain(err, apperrors.DomainProvisioning, apperrors.ErrCodeProvisionFailed, "cloud provisioner failed", true)
	}

	rollback.allocated["cloud"] = true
	rollback.serverID = provisionedNode.ServerID

	// Report sub-phase: waiting for cloud-init
	_ = ps.reportProgress(ctx, nodeID, "cloud_provision", 0.40, "Waiting for cloud-init to complete",
		map[string]interface{}{
			"server_id":  provisionedNode.ServerID,
			"ip_address": provisionedNode.IPAddress,
		})

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
	node.ServerID = provisionedNode.ServerID
	node.IPAddress = provisionedNode.IPAddress
	node.ServerPublicKey = provisionedNode.PublicKey
	node.UpdatedAt = time.Now()
	node.Version++

	if err := ps.repository.Update(ctx, node); err != nil {
		ps.logger.DebugContext(ctx, "failed to update node details", slog.String("error", err.Error()))
		return err // Repo returns DomainError
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
			return apperrors.NewInfrastructureError(apperrors.ErrCodeSSHTimeout, "timed out waiting for node to be ready", true, readinessCtx.Err()).
				WithMetadata("node_id", nodeID).WithMetadata("ip", nodeIP)
		case <-ticker.C:
			attemptCount++
			ps.logger.DebugContext(ctx, "checking node readiness",
				slog.Int("attempt", attemptCount))

			_ = ps.reportProgress(ctx, nodeID, "ssh_connection", 0.75,
				fmt.Sprintf("Checking node readiness (attempt %d)", attemptCount), nil)

			health, err := ps.nodeInteractor.CheckNodeHealth(readinessCtx, nodeIP)
			if err == nil && health.IsHealthy {
				ps.logger.InfoContext(ctx, "node is ready", slog.String("node_id", nodeID))
				return nil
			}

			if err != nil {
				ps.logger.DebugContext(ctx, "node not ready yet",
					slog.String("error", err.Error()))
			}
		}
	}
}

func (ps *ProvisioningService) activateNode(ctx context.Context, node *Node, rollback *provisioningRollback) error {
	if err := node.UpdateStatus(StatusActive); err != nil {
		return err // Model returns DomainError
	}

	if err := ps.repository.Update(ctx, node); err != nil {
		return err // Repo returns DomainError
	}

	rollback.allocated["active"] = true
	return nil
}

func (ps *ProvisioningService) performRollback(ctx context.Context, rollback *provisioningRollback) {
	if !ps.config.CleanupOnFailure {
		ps.logger.DebugContext(ctx, "cleanup on failure disabled, skipping rollback")
		return
	}

	ps.logger.InfoContext(ctx, "performing provisioning rollback", slog.String("node_id", rollback.nodeID))
	rollback.execute(ctx)
}

// provisioningRollback handles cleanup operations for failed provisioning
type provisioningRollback struct {
	nodeID    string
	serverID  string
	service   *ProvisioningService
	logger    *applogger.Logger // <-- Changed
	allocated map[string]bool
}

// execute performs the rollback operations in reverse order
func (r *provisioningRollback) execute(ctx context.Context) {
	// ... (No change in logic, but log calls are now correct) ...

	if r.allocated["cloud"] && r.serverID != "" {
		r.logger.DebugContext(ctx, "rolling back cloud infrastructure", slog.String("server_id", r.serverID))
		if err := r.service.cloudProvisioner.DestroyNode(ctx, r.serverID); err != nil {
			r.logger.ErrorCtx(ctx, "failed to rollback cloud infrastructure", err,
				slog.String("server_id", r.serverID))
		}
	}

	if r.allocated["subnet"] {
		r.logger.DebugContext(ctx, "rolling back subnet allocation", slog.String("node_id", r.nodeID))
		if err := r.service.ipService.ReleaseNodeSubnet(ctx, r.nodeID); err != nil {
			r.logger.ErrorCtx(ctx, "failed to rollback subnet allocation", err,
				slog.String("node_id", r.nodeID))
		}
	}

	r.logger.DebugContext(ctx, "rolling back database record", slog.String("node_id", r.nodeID))
	if err := r.service.repository.Delete(ctx, r.nodeID); err != nil {
		r.logger.ErrorCtx(ctx, "failed to rollback database record", err,
			slog.String("node_id", r.nodeID))
	}

	r.logger.InfoContext(ctx, "provisioning rollback completed", slog.String("node_id", r.nodeID))
}
