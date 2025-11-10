package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// ProvisioningOrchestrator is the single unified orchestrator for all node provisioning
type ProvisioningOrchestrator struct {
	domainService  *node.ProvisioningService
	nodeService    node.NodeService
	eventPublisher *events.ProvisioningEventPublisher
	stateTracker   *events.NodeStateTracker
	logger         *applogger.Logger
	config         OrchestratorConfig

	mu                  sync.RWMutex
	historicalDurations map[string]time.Duration
	unsubscribeFunc     *sharedevents.UnsubscribeFunc
}

// OrchestratorConfig defines configuration for the provisioning orchestrator
type OrchestratorConfig struct {
	WorkerTimeout          time.Duration
	ETAHistoryRetention    int
	DefaultProvisioningETA time.Duration
	MaxConcurrentJobs      int
}

// NewProvisioningOrchestratorWithConfig creates an orchestrator with custom configuration
func NewProvisioningOrchestratorWithConfig(
	domainService *node.ProvisioningService,
	nodeService node.NodeService,
	eventPublisher *events.ProvisioningEventPublisher,
	config OrchestratorConfig,
	logger *applogger.Logger,
) *ProvisioningOrchestrator {
	statueTracker := events.NewNodeStateTracker(eventPublisher, logger)
	return &ProvisioningOrchestrator{
		domainService:       domainService,
		nodeService:         nodeService,
		eventPublisher:      eventPublisher,
		stateTracker:        statueTracker,
		logger:              logger.WithComponent("provisioning.orchestrator"),
		config:              config,
		historicalDurations: getDefaultPhaseDurations(),
	}
}

// ProvisionNodeSync provisions a node synchronously (blocking call)
func (o *ProvisioningOrchestrator) ProvisionNodeSync(ctx context.Context) (*node.Node, error) {
	op := o.logger.StartOp(ctx, "ProvisionNodeSync")

	if o.IsProvisioning() {
		err := apperrors.NewSystemError(apperrors.ErrCodeProvisionInProgress, "provisioning already in progress", false, nil)
		op.Fail(err, "provisioning already in progress")
		return nil, err
	}

	provisionedNode, err := o.domainService.ProvisionNode(ctx)
	if err != nil {
		op.Fail(err, "synchronous provisioning failed")
		return nil, err
	}

	o.updateHistoricalDuration("total", time.Since(op.StartTime))
	op.Complete("synchronous provisioning successful", slog.String("node_id", provisionedNode.ID))
	return provisionedNode, nil
}

// ProvisionNodeAsync triggers asynchronous provisioning via event
func (o *ProvisioningOrchestrator) ProvisionNodeAsync(ctx context.Context) error {
	op := o.logger.StartOp(ctx, "ProvisionNodeAsync")

	if o.IsProvisioning() {
		err := apperrors.NewSystemError(apperrors.ErrCodeProvisionInProgress, "provisioning already in progress", false, nil)
		op.Fail(err, "provisioning already in progress")
		return err
	}

	requestID := fmt.Sprintf("provision-%d", time.Now().UnixNano())
	if err := o.eventPublisher.PublishProvisionRequested(ctx, requestID, "rotator"); err != nil {
		err = apperrors.WrapWithDomain(err, apperrors.DomainEvent, "publish_failed", "failed to publish provision request", true)
		op.Fail(err, "failed to publish provision request")
		return err
	}

	op.Complete("async provisioning request published", slog.String("request_id", requestID))
	return nil
}

// DestroyNode destroys a node with coordinated cleanup
func (o *ProvisioningOrchestrator) DestroyNode(ctx context.Context, nodeID string) error {
	op := o.logger.StartOp(ctx, "DestroyNode", slog.String("node_id", nodeID))
	if err := o.domainService.DestroyNode(ctx, nodeID); err != nil {
		op.Fail(err, "node destruction failed")
		return err
	}
	op.Complete("node destroyed successfully")
	return nil
}

// StartWorker starts the background worker that handles async provisioning requests
func (o *ProvisioningOrchestrator) StartWorker(ctx context.Context) {
	o.logger.InfoContext(ctx, "starting provisioning orchestrator worker")

	unsubscribeFunc, err := o.eventPublisher.OnProvisionRequested(func(eventCtx context.Context, e sharedevents.Event) error {
		return o.handleProvisionRequest(eventCtx, e)
	})
	if err != nil {
		o.logger.ErrorCtx(ctx, "failed to subscribe to provision request events", err)
		return
	}
	o.unsubscribeFunc = &unsubscribeFunc

	go func() {
		<-ctx.Done()
		o.logger.Info("stopping provisioning orchestrator worker due to context cancellation")
		if o.unsubscribeFunc != nil {
			(*o.unsubscribeFunc)()
			o.logger.Info("unsubscribed from provision request events")
		}
	}()
	o.logger.InfoContext(ctx, "provisioning orchestrator worker started")
}

// handleProvisionRequest handles incoming async provision requests
func (o *ProvisioningOrchestrator) handleProvisionRequest(ctx context.Context, e sharedevents.Event) error {
	if o.IsProvisioning() {
		o.logger.InfoContext(ctx, "provisioning already active, ignoring request", "event_type", e.Type())
		return nil
	}
	o.logger.InfoContext(ctx, "received async provision request, starting worker", "event_data", e.Metadata())
	go o.runProvisioningWithCleanup(ctx)
	return nil
}

// runProvisioningWithCleanup executes provisioning and cleans up state afterwards
func (o *ProvisioningOrchestrator) runProvisioningWithCleanup(ctx context.Context) {
	op := o.logger.StartOp(ctx, "run_async_provisioning")
	provisionedNode, err := o.domainService.ProvisionNode(ctx)
	if err != nil {
		op.Fail(err, "async provisioning failed")
		return
	}
	o.updateHistoricalDuration("total", time.Since(op.StartTime))
	op.Complete("async provisioning successful", slog.String("node_id", provisionedNode.ID))
}

// Status Query Methods (delegated to NodeStateTracker)

func (o *ProvisioningOrchestrator) IsProvisioning() bool {
	return o.stateTracker.IsProvisioning()
}

func (o *ProvisioningOrchestrator) GetCurrentStatus() *events.ProvisioningNodeState {
	return o.stateTracker.GetActiveNode()
}

func (o *ProvisioningOrchestrator) GetEstimatedWaitTime() time.Duration {
	waitTime := o.stateTracker.GetEstimatedWaitTime()
	if waitTime > 0 {
		return waitTime
	}
	return o.config.DefaultProvisioningETA
}

func (o *ProvisioningOrchestrator) GetProgress() float64 {
	return o.stateTracker.GetProgress()
}

// Helper Methods

func (o *ProvisioningOrchestrator) updateHistoricalDuration(phase string, duration time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if existing, exists := o.historicalDurations[phase]; exists {
		weight := float64(o.config.ETAHistoryRetention) / (float64(o.config.ETAHistoryRetention) + 1.0)
		newDuration := time.Duration(weight*float64(existing) + (1.0-weight)*float64(duration))
		o.historicalDurations[phase] = newDuration
	} else {
		o.historicalDurations[phase] = duration
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
