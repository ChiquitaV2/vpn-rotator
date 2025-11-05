package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
)

// ProvisioningOrchestrator is the single unified orchestrator for all node provisioning
// It handles both synchronous and asynchronous provisioning with event-driven progress tracking
//
// Responsibilities:
//   - Coordinates provisioning workflows (sync and async)
//   - Translates domain progress reports into events
//   - Prevents duplicate provisioning attempts
//   - Orchestrates the provisioning process
//
// Architecture:
//   - Wraps domain layer node.ProvisioningService (does actual work)
//   - Consumes events.ProgressReporter interface
//   - Publishes events to ProvisioningEventBus (application layer consumes)
//   - Uses NodeStateTracker for all state queries (no internal state)
type ProvisioningOrchestrator struct {
	domainService  *node.ProvisioningService
	nodeService    node.NodeService
	eventPublisher *events.ProvisioningEventPublisher
	stateTracker   *events.NodeStateTracker
	logger         *slog.Logger
	config         OrchestratorConfig

	// Historical data for ETA calculations
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
	logger *slog.Logger,
) *ProvisioningOrchestrator {
	statueTracker := events.NewNodeStateTracker(eventPublisher)
	return &ProvisioningOrchestrator{
		domainService:       domainService,
		nodeService:         nodeService,
		eventPublisher:      eventPublisher,
		stateTracker:        statueTracker,
		logger:              logger,
		config:              config,
		historicalDurations: getDefaultPhaseDurations(),
	}
}

// ProvisionNodeSync provisions a node synchronously (blocking call)
// This is used when immediate provisioning is required (e.g., during rotation)
func (o *ProvisioningOrchestrator) ProvisionNodeSync(ctx context.Context) (*node.Node, error) {
	o.logger.Info("starting synchronous node provisioning")

	// Check if already provisioning
	if o.IsProvisioning() {
		return nil, fmt.Errorf("provisioning already in progress")
	}

	// Provision via domain service (it will call back to us via ProgressReporter interface)
	// The state tracking is handled by events published through the ProgressReporter
	startTime := time.Now()
	provisionedNode, err := o.domainService.ProvisionNode(ctx)
	if err != nil {
		return nil, err
	}

	// Update historical data
	duration := time.Since(startTime)
	o.updateHistoricalDuration("total", duration)

	o.logger.Info("synchronous provisioning completed",
		slog.String("node_id", provisionedNode.ID),
		slog.Duration("duration", duration))

	return provisionedNode, nil
}

// ProvisionNodeAsync triggers asynchronous provisioning via event
// Returns immediately, provisioning happens in background worker
func (o *ProvisioningOrchestrator) ProvisionNodeAsync(ctx context.Context) error {
	o.logger.Info("triggering asynchronous node provisioning")

	// Check if already provisioning
	if o.IsProvisioning() {
		return fmt.Errorf("provisioning already in progress")
	}

	// Publish provision request event
	requestID := fmt.Sprintf("provision-%d", time.Now().UnixNano())
	if err := o.eventPublisher.PublishProvisionRequested(ctx, requestID, "rotator"); err != nil {
		return fmt.Errorf("failed to publish provision request: %w", err)
	}

	o.logger.Info("async provisioning request published", slog.String("request_id", requestID))
	return nil
}

// DestroyNode destroys a node with coordinated cleanup
func (o *ProvisioningOrchestrator) DestroyNode(ctx context.Context, nodeID string) error {
	o.logger.Info("destroying node", slog.String("node_id", nodeID))
	return o.domainService.DestroyNode(ctx, nodeID)
}

// StartWorker starts the background worker that handles async provisioning requests
// This should be called once during service initialization
func (o *ProvisioningOrchestrator) StartWorker(ctx context.Context) {
	o.logger.Info("starting provisioning orchestrator worker")

	// Subscribe to provision request events
	unsubscribeFunc, err := o.eventPublisher.OnProvisionRequested(func(eventCtx context.Context, e sharedevents.Event) error {
		return o.handleProvisionRequest(eventCtx, e)
	})
	if err != nil {
		o.logger.Error("failed to subscribe to provision request events", slog.String("error", err.Error()))
		return
	}
	o.unsubscribeFunc = &unsubscribeFunc

	// Worker runs until context is cancelled
	go func() {
		<-ctx.Done()
		o.logger.Info("stopping provisioning orchestrator worker due to context cancellation")

		// Unsubscribe from events
		if o.unsubscribeFunc != nil {
			(*o.unsubscribeFunc)()
			o.logger.Info("unsubscribed from provision request events")
		}
	}()
	o.logger.Info("provisioning orchestrator worker started")

}

// handleProvisionRequest handles incoming async provision requests
func (o *ProvisioningOrchestrator) handleProvisionRequest(ctx context.Context, e sharedevents.Event) error {
	// Prevent duplicate provisioning by checking the state tracker
	if o.IsProvisioning() {
		o.logger.Info("provisioning already active, ignoring request")
		return nil
	}

	// Run provisioning in background
	go o.runProvisioningWithCleanup(ctx)

	return nil
}

// runProvisioningWithCleanup executes provisioning and cleans up state afterwards
func (o *ProvisioningOrchestrator) runProvisioningWithCleanup(ctx context.Context) {
	startTime := time.Now()

	// Provision via domain service (state is managed through events via ProgressReporter)
	provisionedNode, err := o.domainService.ProvisionNode(ctx)

	if err != nil {
		o.logger.Error("async provisioning failed", slog.String("error", err.Error()))
		return
	}

	// Update historical data
	duration := time.Since(startTime)
	o.updateHistoricalDuration("total", duration)

	o.logger.Info("async provisioning completed",
		slog.String("node_id", provisionedNode.ID),
		slog.Duration("duration", duration))
}

// Status Query Methods (delegated to NodeStateTracker)

// IsProvisioning returns true if provisioning is currently active
func (o *ProvisioningOrchestrator) IsProvisioning() bool {
	return o.stateTracker.IsProvisioning()
}

// GetCurrentStatus returns the current provisioning status
func (o *ProvisioningOrchestrator) GetCurrentStatus() *events.ProvisioningNodeState {
	return o.stateTracker.GetActiveNode()
}

// GetEstimatedWaitTime returns estimated time remaining for current provisioning
func (o *ProvisioningOrchestrator) GetEstimatedWaitTime() time.Duration {
	waitTime := o.stateTracker.GetEstimatedWaitTime()
	if waitTime > 0 {
		return waitTime
	}
	return o.config.DefaultProvisioningETA
}

// GetProgress returns current progress (0.0 to 1.0)
func (o *ProvisioningOrchestrator) GetProgress() float64 {
	return o.stateTracker.GetProgress()
}

// Helper Methods

// updateHistoricalDuration updates historical duration data (thread-safe)
func (o *ProvisioningOrchestrator) updateHistoricalDuration(phase string, duration time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if existing, exists := o.historicalDurations[phase]; exists {
		// Exponential moving average
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
