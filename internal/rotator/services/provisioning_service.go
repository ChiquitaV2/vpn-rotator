package services

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// CloudProvisioner defines the interface for cloud infrastructure provisioning
type CloudProvisioner interface {
	ProvisionNode(ctx context.Context, config node.ProvisioningConfig) (*node.ProvisionedNode, error)
	DestroyNode(ctx context.Context, serverID string) error
	GetNodeStatus(ctx context.Context, serverID string) (*node.CloudNodeStatus, error)
}

// ProvisioningService is the single unified orchestrator for all node provisioning
type ProvisioningService struct {
	nodeService      node.NodeService
	repository       node.NodeRepository
	cloudProvisioner CloudProvisioner
	nodeInteractor   node.HealthChecker
	ipService        ip.Service
	eventPublisher   *events.ProvisioningEventPublisher
	stateTracker     *node.ProvisioningStateTracker
	logger           *applogger.Logger
	config           ProvisioningServiceConfig

	mu                  sync.RWMutex
	historicalDurations map[string]time.Duration
	unsubscribeFunc     *sharedevents.UnsubscribeFunc
}

// ProvisioningServiceConfig contains configuration for the provisioning service
type ProvisioningServiceConfig struct {
	WorkerTimeout          time.Duration
	ETAHistoryRetention    int
	DefaultProvisioningETA time.Duration
	MaxConcurrentJobs      int
	ProvisioningTimeout    time.Duration     `json:"provisioning_timeout"`
	ReadinessTimeout       time.Duration     `json:"readiness_timeout"`
	ReadinessCheckInterval time.Duration     `json:"readiness_check_interval"`
	MaxRetryAttempts       int               `json:"max_retry_attempts"`
	RetryBackoff           time.Duration     `json:"retry_backoff"`
	CleanupOnFailure       bool              `json:"cleanup_on_failure"`
	DefaultRegion          string            `json:"default_region"`
	DefaultInstanceType    string            `json:"default_instance_type"`
	DefaultImageID         string            `json:"default_image_id"`
	DefaultSSHKey          string            `json:"default_ssh_key"`
	DefaultTags            map[string]string `json:"default_tags"`
}

// NewProvisioningService creates a new provisioning service
func NewProvisioningService(
	nodeService node.NodeService,
	repository node.NodeRepository,
	cloudProvisioner CloudProvisioner,
	nodeInteractor node.HealthChecker,
	ipService ip.Service,
	eventPublisher *events.ProvisioningEventPublisher,
	config ProvisioningServiceConfig,
	logger *applogger.Logger,
) *ProvisioningService {
	statueTracker := node.NewProvisioningStateTracker(eventPublisher, logger)
	return &ProvisioningService{
		nodeService:         nodeService,
		repository:          repository,
		cloudProvisioner:    cloudProvisioner,
		nodeInteractor:      nodeInteractor,
		ipService:           ipService,
		eventPublisher:      eventPublisher,
		stateTracker:        statueTracker,
		logger:              logger.WithComponent("provisioning.service"),
		config:              config,
		historicalDurations: getDefaultPhaseDurations(),
	}
}

// ProvisionNodeSync provisions a node synchronously (blocking call)
func (s *ProvisioningService) ProvisionNodeSync(ctx context.Context) (*node.Node, error) {
	op := s.logger.StartOp(ctx, "ProvisionNodeSync")

	if s.IsProvisioning() {
		err := apperrors.NewSystemError(apperrors.ErrCodeProvisionInProgress, "provisioning already in progress", false, nil)
		op.Fail(err, "provisioning already in progress")
		return nil, err
	}

	provisionedNode, err := s.provisionNode(ctx)
	if err != nil {
		op.Fail(err, "synchronous provisioning failed")
		return nil, err
	}

	s.updateHistoricalDuration("total", time.Since(op.StartTime))
	op.Complete("synchronous provisioning successful", slog.String("node_id", provisionedNode.ID))
	return provisionedNode, nil
}

// ProvisionNodeAsync triggers asynchronous provisioning via event
func (s *ProvisioningService) ProvisionNodeAsync(ctx context.Context) error {
	op := s.logger.StartOp(ctx, "ProvisionNodeAsync")

	if s.IsProvisioning() {
		err := apperrors.NewSystemError(apperrors.ErrCodeProvisionInProgress, "provisioning already in progress", false, nil)
		op.Fail(err, "provisioning already in progress")
		return err
	}

	requestID := fmt.Sprintf("provision-%d", time.Now().UnixNano())
	if err := s.eventPublisher.PublishProvisionRequested(ctx, requestID, "rotator"); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainEvent, "publish_failed", "failed to publish provision request", true)
		op.Fail(err, "failed to publish provision request")
		return err
	}

	op.Complete("async provisioning request published", slog.String("request_id", requestID))
	return nil
}

// DestroyNode destroys a node with coordinated cleanup
func (s *ProvisioningService) DestroyNode(ctx context.Context, nodeID string) error {
	op := s.logger.StartOp(ctx, "DestroyNode", slog.String("node_id", nodeID))
	if err := s.destroyNode(ctx, nodeID); err != nil {
		op.Fail(err, "node destruction failed")
		return err
	}
	op.Complete("node destroyed successfully")
	return nil
}

// StartWorker starts the background worker that handles async provisioning requests
func (s *ProvisioningService) StartWorker(ctx context.Context) {
	s.logger.InfoContext(ctx, "starting provisioning service worker")

	unsubscribeFunc, err := s.eventPublisher.OnProvisionRequested(func(eventCtx context.Context, e sharedevents.Event) error {
		return s.handleProvisionRequest(eventCtx, e)
	})
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to subscribe to provision request events", err)
		return
	}
	s.unsubscribeFunc = &unsubscribeFunc

	go func() {
		<-ctx.Done()
		s.logger.Info("stopping provisioning service worker due to context cancellation")
		if s.unsubscribeFunc != nil {
			(*s.unsubscribeFunc)()
			s.logger.Info("unsubscribed from provision request events")
		}
	}()
	s.logger.InfoContext(ctx, "provisioning service worker started")
}

// handleProvisionRequest handles incoming async provision requests
func (s *ProvisioningService) handleProvisionRequest(ctx context.Context, e sharedevents.Event) error {
	if s.IsProvisioning() {
		s.logger.InfoContext(ctx, "provisioning already active, ignoring request", "event_type", e.Type())
		return nil
	}
	s.logger.InfoContext(ctx, "received async provision request, starting worker", "event_data", e.Metadata())
	go s.runProvisioningWithCleanup(ctx)
	return nil
}

// runProvisioningWithCleanup executes provisioning and cleans up state afterwards
func (s *ProvisioningService) runProvisioningWithCleanup(ctx context.Context) {
	op := s.logger.StartOp(ctx, "run_async_provisioning")
	provisionedNode, err := s.provisionNode(ctx)
	if err != nil {
		op.Fail(err, "async provisioning failed")
		return
	}
	s.updateHistoricalDuration("total", time.Since(op.StartTime))
	op.Complete("async provisioning successful", slog.String("node_id", provisionedNode.ID))
}

// Status Query Methods (delegated to NodeStateTracker)

func (s *ProvisioningService) IsProvisioning() bool {
	return s.stateTracker.IsProvisioning()
}

func (s *ProvisioningService) GetCurrentStatus() *node.ProvisioningState {
	return s.stateTracker.GetActiveProvisioning()
}

func (s *ProvisioningService) GetEstimatedWaitTime() time.Duration {
	waitTime := s.stateTracker.GetEstimatedWaitTime()
	if waitTime > 0 {
		return waitTime
	}
	return s.config.DefaultProvisioningETA
}

func (s *ProvisioningService) GetProgress() float64 {
	return s.stateTracker.GetProgress()
}

// GetProvisioningStateTracker returns the state tracker for external queries
func (s *ProvisioningService) GetProvisioningStateTracker() *node.ProvisioningStateTracker {
	return s.stateTracker
}

// Helper Methods

func (s *ProvisioningService) updateHistoricalDuration(phase string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.historicalDurations[phase]; exists {
		weight := float64(s.config.ETAHistoryRetention) / (float64(s.config.ETAHistoryRetention) + 1.0)
		newDuration := time.Duration(weight*float64(existing) + (1.0-weight)*float64(duration))
		s.historicalDurations[phase] = newDuration
	} else {
		s.historicalDurations[phase] = duration
	}
}

func getDefaultPhaseDurations() map[string]time.Duration {
	return map[string]time.Duration{
		"total":           time.Minute * 3,
		"validation":      time.Second * 5,
		"ip_allocation":   time.Second * 10,
		"cloud_provision": time.Minute * 2,
		"ssh_connection":  time.Second * 30,
		"wireguard_setup": time.Second * 15,
		"health_check":    time.Second * 30,
	}
}

// provisionNode provisions a new node with coordinated IP allocation, cloud provisioning, and setup
func (s *ProvisioningService) provisionNode(ctx context.Context) (*node.Node, error) {
	op := s.logger.StartOp(ctx, "ProvisionNode")

	// Create timeout context for the entire provisioning process
	provisioningCtx, cancel := context.WithTimeout(ctx, s.config.ProvisioningTimeout)
	defer cancel()

	// Generate unique node ID
	nodeID := fmt.Sprintf("node-%d", time.Now().UnixNano())

	// Phase 1: Initialization (5%)
	_ = s.reportPhaseStart(provisioningCtx, nodeID, "initialization", "Starting node provisioning")

	// Create database record immediately
	n, err := s.createProvisioningRecord(provisioningCtx, nodeID)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "initialization", err)
		s.logger.ErrorCtx(ctx, "failed to create provisioning record", err)
		op.Fail(err, "failed to create provisioning record")
		return nil, err
	}

	s.logger.DebugContext(ctx, "created provisioning node record", slog.String("node_id", nodeID))
	_ = s.reportProgress(provisioningCtx, nodeID, "initialization", 0.05, "Database record created", nil)

	// Initialize rollback context for cleanup
	rollback := &provisioningRollback{
		nodeID:    nodeID,
		service:   s,
		logger:    s.logger, // Pass the logger
		allocated: make(map[string]bool),
	}

	// Phase 2: Subnet Allocation (10%)
	_ = s.reportPhaseStart(provisioningCtx, nodeID, "subnet_allocation", "Allocating IP subnet")

	subnet, err := s.allocateSubnet(provisioningCtx, nodeID, rollback)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "subnet_allocation", err)
		s.performRollback(provisioningCtx, rollback)
		s.logger.ErrorCtx(ctx, "failed to allocate subnet", err)
		op.Fail(err, "failed to allocate subnet")
		return nil, err
	}
	_ = s.reportProgress(provisioningCtx, nodeID, "subnet_allocation", 0.10, "Subnet allocated", nil)

	// Phase 3: Cloud Provisioning (65%)
	_ = s.reportPhaseStart(provisioningCtx, nodeID, "cloud_provision", "Provisioning cloud infrastructure")

	provisionedNode, err := s.provisionCloudInfrastructure(provisioningCtx, nodeID, subnet, rollback)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "cloud_provision", err)
		s.performRollback(provisioningCtx, rollback)
		s.logger.ErrorCtx(ctx, "failed to provision cloud infrastructure", err)
		op.Fail(err, "failed to provision cloud infrastructure")
		return nil, err
	}
	_ = s.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.65, "Cloud server created", nil)

	// Phase 4: Update Database Details (70%)
	_ = s.reportProgress(provisioningCtx, nodeID, "cloud_provision", 0.70, "Updating database with provisioned details", nil)

	err = s.updateNodeDetails(provisioningCtx, n, provisionedNode, rollback)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "cloud_provision", err)
		s.performRollback(provisioningCtx, rollback)
		s.logger.ErrorCtx(ctx, "failed to update node details", err)
		op.Fail(err, "failed to update node details")
		return nil, err
	}

	// Phase 5: SSH Connection & Readiness (85%)
	_ = s.reportPhaseStart(provisioningCtx, nodeID, "ssh_connection", "Waiting for SSH and node readiness")

	err = s.waitForNodeReadiness(provisioningCtx, nodeID, provisionedNode.IPAddress, rollback)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "ssh_connection", err)
		s.performRollback(provisioningCtx, rollback)
		s.logger.ErrorCtx(ctx, "node readiness check failed", err)
		op.Fail(err, "node readiness check failed")
		return nil, err
	}
	_ = s.reportProgress(provisioningCtx, nodeID, "ssh_connection", 0.85, "Node is ready and accessible", nil)

	// Phase 6: Health Check (90%)
	// ... (This is part of readiness)
	_ = s.reportProgress(provisioningCtx, nodeID, "health_check", 0.90, "Health checks passed", nil)

	// Phase 7: Activation (95%)
	_ = s.reportPhaseStart(provisioningCtx, nodeID, "activation", "Activating node")

	err = s.activateNode(provisioningCtx, n, rollback)
	if err != nil {
		_ = s.reportError(provisioningCtx, nodeID, "activation", err)
		s.performRollback(provisioningCtx, rollback)
		s.logger.ErrorCtx(ctx, "failed to activate node", err)
		op.Fail(err, "failed to activate node")
		return nil, err
	}
	_ = s.reportProgress(provisioningCtx, nodeID, "activation", 0.95, "Node activated successfully", nil)

	// Phase 8: Completion (100%)
	duration := time.Since(op.StartTime)
	_ = s.reportProgress(provisioningCtx, nodeID, "completed", 1.0, "Node provisioning completed",
		map[string]interface{}{"duration_seconds": duration.Seconds()})

	// Report to event bus
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishProvisionCompleted(ctx, nodeID, provisionedNode.ServerID, provisionedNode.IPAddress, duration)
	}

	// Return the final node state
	finalNode, err := s.repository.GetByID(provisioningCtx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get final node state, returning in-memory node", err,
			slog.String("node_id", nodeID))
		op.Complete("node provisioning completed with in-memory node returned")
		return n, nil // Return the node we have
	}

	op.Complete("node provisioning completed", slog.String("node_id", nodeID))
	return finalNode, nil
}

// destroyNode destroys a node with coordinated cleanup
func (s *ProvisioningService) destroyNode(ctx context.Context, nodeID string) error {
	op := s.logger.StartOp(ctx, "DestroyNode", slog.String("node_id", nodeID))

	// Get node from repository
	n, err := s.repository.GetByID(ctx, nodeID)
	if err != nil {
		s.logger.ErrorCtx(ctx, "failed to get node", err)
		op.Fail(err, "failed to get node")
		return err // Repo returns DomainError
	}

	// Validate node can be destroyed
	if err := n.ValidateForDestruction(); err != nil {
		s.logger.ErrorCtx(ctx, "node validation for destruction failed", err)
		op.Fail(err, "node validation for destruction failed")
		return err // Model returns DomainError
	}

	// Update status to destroying
	if err := n.UpdateStatus(node.StatusDestroying); err != nil {
		s.logger.ErrorCtx(ctx, "failed to update node status to destroying", err)
		op.Fail(err, "failed to update node status to destroying")
		return err // Model returns DomainError
	}

	if err := s.repository.Update(ctx, n); err != nil {
		s.logger.ErrorCtx(ctx, "failed to save destroying status", err)
		op.Fail(err, "failed to save destroying status")
		return err // Repo returns DomainError
	}

	// Step 1: Release subnet allocation
	if err = s.ipService.ReleaseNodeSubnet(ctx, nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "failed to release node subnet, continuing destruction", err,
			slog.String("node_id", nodeID))
		// Continue with destruction
	}

	// Step 2: Destroy cloud infrastructure
	if n.ServerID != "" {
		if err = s.cloudProvisioner.DestroyNode(ctx, n.ServerID); err != nil {
			s.logger.ErrorCtx(ctx, "failed to destroy cloud infrastructure, continuing with db delete", err,
				slog.String("server_id", n.ServerID))
			// Continue with database cleanup
		}
	}

	// Step 3: Remove from database
	if err = s.repository.Delete(ctx, nodeID); err != nil {
		s.logger.ErrorCtx(ctx, "failed to delete node from database", err)
		op.Fail(err, "failed to delete node from database")
		return err
	}

	op.Complete("node destroyed successfully")
	return nil
}

// Progress reporting helper methods

func (s *ProvisioningService) reportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	if s.eventPublisher == nil {
		return nil // Silently skip
	}
	return s.eventPublisher.PublishProvisionProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (s *ProvisioningService) reportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	if s.eventPublisher == nil {
		return nil
	}
	return s.eventPublisher.PublishProvisionProgress(ctx, nodeID, phase, 0, message, nil)
}

func (s *ProvisioningService) reportError(ctx context.Context, nodeID, phase string, err error) error {
	if s.eventPublisher == nil {
		return nil
	}
	return s.eventPublisher.PublishProvisionFailed(ctx, nodeID, phase, err) // Pass the full error
}

// Helper methods for provisioning steps

func (s *ProvisioningService) createProvisioningRecord(ctx context.Context, nodeID string) (*node.Node, error) {
	n, err := node.NewNode(nodeID, "0.0.0.0", nodeID, 51820) // Placeholder values
	if err != nil {
		return nil, err // Will be a DomainError from NewNode
	}

	// Save to repository
	if err := s.repository.Create(ctx, n); err != nil {
		s.logger.DebugContext(ctx, "failed to create provisioning record", slog.String("error", err.Error()))
		return nil, err // Repo returns DomainError
	}
	return n, nil
}

func (s *ProvisioningService) allocateSubnet(ctx context.Context, nodeID string, rollback *provisioningRollback) (*net.IPNet, error) {
	subnet, err := s.ipService.AllocateNodeSubnet(ctx, nodeID)
	if err != nil {
		return nil, err // ipService should return a DomainError
	}
	rollback.allocated["subnet"] = true
	return subnet, nil
}

func (s *ProvisioningService) provisionCloudInfrastructure(ctx context.Context, nodeID string, subnet *net.IPNet, rollback *provisioningRollback) (*node.ProvisionedNode, error) {
	// Report sub-phase: preparing configuration
	_ = s.reportProgress(ctx, nodeID, "cloud_provision", 0.15, "Preparing cloud configuration", nil)

	// Create provisioning config - location and instance type will be determined by the provisioner
	config := node.ProvisioningConfig{
		Region:       s.getConfigValue(s.config.DefaultRegion, "nbg1"),
		InstanceType: s.getConfigValue(s.config.DefaultInstanceType, "cx11"),
		ImageID:      s.getConfigValue(s.config.DefaultImageID, "ubuntu-22.04"),
		SSHKeyName:   "",
		Tags:         s.getDefaultTags(),
		Subnet:       subnet,
		SSHPublicKey: s.config.DefaultSSHKey,
	}

	// Report sub-phase: calling cloud provisioner
	_ = s.reportProgress(ctx, nodeID, "cloud_provision", 0.20, "Creating cloud server", nil)

	provisionedNode, err := s.cloudProvisioner.ProvisionNode(ctx, config)
	if err != nil {
		// Assume cloudProvisioner returns a DomainError or wrap it
		return nil, apperrors.WrapWithDomain(err, apperrors.DomainProvisioning, apperrors.ErrCodeProvisionFailed, "cloud provisioner failed", true)
	}

	rollback.allocated["cloud"] = true
	rollback.serverID = provisionedNode.ServerID

	// Report sub-phase: waiting for cloud-init
	_ = s.reportProgress(ctx, nodeID, "cloud_provision", 0.40, "Waiting for cloud-init to complete",
		map[string]interface{}{
			"server_id":  provisionedNode.ServerID,
			"ip_address": provisionedNode.IPAddress,
		})

	_ = s.reportProgress(ctx, nodeID, "cloud_provision", 0.60, "Cloud-init completed", nil)

	return provisionedNode, nil
}

// Helper methods for configuration

func (s *ProvisioningService) getConfigValue(configValue, defaultValue string) string {
	if configValue != "" {
		return configValue
	}
	return defaultValue
}

func (s *ProvisioningService) getDefaultTags() map[string]string {
	tags := map[string]string{
		"service": "vpn-rotator",
		"type":    "vpn-node",
	}

	// Merge with configured default tags
	for k, v := range s.config.DefaultTags {
		tags[k] = v
	}

	return tags
}

func (s *ProvisioningService) updateNodeDetails(ctx context.Context, n *node.Node, provisionedNode *node.ProvisionedNode, rollback *provisioningRollback) error {
	n.ServerID = provisionedNode.ServerID
	n.IPAddress = provisionedNode.IPAddress
	n.ServerPublicKey = provisionedNode.PublicKey
	n.UpdatedAt = time.Now()
	n.Version++

	if err := s.repository.Update(ctx, n); err != nil {
		s.logger.DebugContext(ctx, "failed to update node details", slog.String("error", err.Error()))
		return err // Repo returns DomainError
	}
	rollback.allocated["details"] = true
	return nil
}

func (s *ProvisioningService) waitForNodeReadiness(ctx context.Context, nodeID, nodeIP string, rollback *provisioningRollback) error {
	readinessCtx, cancel := context.WithTimeout(ctx, s.config.ReadinessTimeout)
	defer cancel()

	ticker := time.NewTicker(s.config.ReadinessCheckInterval)
	defer ticker.Stop()

	attemptCount := 0
	for {
		select {
		case <-readinessCtx.Done():
			return apperrors.NewInfrastructureError(apperrors.ErrCodeSSHTimeout, "timed out waiting for node to be ready", true, readinessCtx.Err()).
				WithMetadata("node_id", nodeID).WithMetadata("ip", nodeIP)
		case <-ticker.C:
			attemptCount++
			s.logger.DebugContext(ctx, "checking node readiness",
				slog.Int("attempt", attemptCount))

			_ = s.reportProgress(ctx, nodeID, "ssh_connection", 0.75,
				fmt.Sprintf("Checking node readiness (attempt %d)", attemptCount), nil)

			health, err := s.nodeInteractor.CheckNodeHealth(readinessCtx, nodeIP)
			if err == nil && health.IsHealthy {
				s.logger.InfoContext(ctx, "node is ready", slog.String("node_id", nodeID))
				return nil
			}

			if err != nil {
				s.logger.DebugContext(ctx, "node not ready yet",
					slog.String("error", err.Error()))
			}
		}
	}
}

func (s *ProvisioningService) activateNode(ctx context.Context, n *node.Node, rollback *provisioningRollback) error {
	if err := n.UpdateStatus(node.StatusActive); err != nil {
		return err // Model returns DomainError
	}

	if err := s.repository.Update(ctx, n); err != nil {
		return err // Repo returns DomainError
	}

	rollback.allocated["active"] = true
	return nil
}

func (s *ProvisioningService) performRollback(ctx context.Context, rollback *provisioningRollback) {
	if !s.config.CleanupOnFailure {
		s.logger.DebugContext(ctx, "cleanup on failure disabled, skipping rollback")
		return
	}

	s.logger.InfoContext(ctx, "performing provisioning rollback", slog.String("node_id", rollback.nodeID))
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
