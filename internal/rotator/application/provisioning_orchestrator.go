package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/gookit/event"
)

// ProvisioningOrchestrator is the single unified orchestrator for all node provisioning
// It handles both synchronous and asynchronous provisioning with event-driven progress tracking
//
// Responsibilities:
//   - Coordinates provisioning workflows (sync and async)
//   - Translates domain progress reports into events
//   - Manages provisioning state and ETA calculations
//   - Prevents duplicate provisioning attempts
//
// Architecture:
//   - Wraps domain layer node.ProvisioningService (does actual work)
//   - Implements events.ProgressReporter interface (domain reports to us)
//   - Publishes events to ProvisioningEventBus (application layer consumes)
//   - Maintains thread-safe in-memory state for progress tracking
type ProvisioningOrchestrator struct {
	domainService *node.ProvisioningService
	nodeService   node.NodeService
	eventBus      *events.ProvisioningEventBus
	logger        *slog.Logger
	config        OrchestratorConfig

	// Thread-safe state management
	mu                  sync.RWMutex
	currentProvisioning *ProvisioningState
	historicalDurations map[string]time.Duration // For ETA calculation
}

// OrchestratorConfig defines configuration for the provisioning orchestrator
type OrchestratorConfig struct {
	WorkerTimeout          time.Duration
	ETAHistoryRetention    int
	DefaultProvisioningETA time.Duration
	MaxConcurrentJobs      int
}

// ProvisioningState represents the current state of an active provisioning operation
type ProvisioningState struct {
	NodeID       string
	Phase        string
	Progress     float64 // 0.0 to 1.0
	StartedAt    time.Time
	EstimatedETA *time.Time
	LastUpdated  time.Time
	ErrorMessage string
	IsActive     bool
}

// NewProvisioningOrchestrator creates a new unified provisioning orchestrator
func NewProvisioningOrchestrator(
	domainService *node.ProvisioningService,
	nodeService node.NodeService,
	eventBus *events.ProvisioningEventBus,
	logger *slog.Logger,
) *ProvisioningOrchestrator {
	defaultConfig := OrchestratorConfig{
		WorkerTimeout:          15 * time.Minute,
		ETAHistoryRetention:    10,
		DefaultProvisioningETA: 3 * time.Minute,
		MaxConcurrentJobs:      1,
	}

	return NewProvisioningOrchestratorWithConfig(domainService, nodeService, eventBus, defaultConfig, logger)
}

// NewProvisioningOrchestratorWithConfig creates an orchestrator with custom configuration
func NewProvisioningOrchestratorWithConfig(
	domainService *node.ProvisioningService,
	nodeService node.NodeService,
	eventBus *events.ProvisioningEventBus,
	config OrchestratorConfig,
	logger *slog.Logger,
) *ProvisioningOrchestrator {
	return &ProvisioningOrchestrator{
		domainService:       domainService,
		nodeService:         nodeService,
		eventBus:            eventBus,
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

	// Set initial state
	o.setProvisioningState(&ProvisioningState{
		IsActive:  true,
		Phase:     "starting",
		Progress:  0.0,
		StartedAt: time.Now(),
	})

	defer func() {
		// Clear state after completion/error
		time.AfterFunc(30*time.Second, func() {
			o.mu.Lock()
			o.currentProvisioning = nil
			o.mu.Unlock()
		})
	}()

	// Provision via domain service (it will call back to us via ProgressReporter interface)
	provisionedNode, err := o.domainService.ProvisionNode(ctx)
	if err != nil {
		o.setError(err)
		return nil, err
	}

	// Update historical data
	duration := time.Since(o.getStartTime())
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
	if err := o.eventBus.PublishProvisionRequested(requestID); err != nil {
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
	o.eventBus.SubscribeToProvisionRequests(event.ListenerFunc(func(e event.Event) error {
		return o.handleProvisionRequest(ctx, e)
	}))

	o.logger.Info("provisioning orchestrator worker started")
}

// handleProvisionRequest handles incoming async provision requests
func (o *ProvisioningOrchestrator) handleProvisionRequest(ctx context.Context, e event.Event) error {
	o.mu.Lock()

	// Prevent duplicate provisioning
	if o.currentProvisioning != nil && o.currentProvisioning.IsActive {
		o.logger.Info("provisioning already active, ignoring request")
		o.mu.Unlock()
		return nil
	}

	// Set initial state
	o.currentProvisioning = &ProvisioningState{
		IsActive:    true,
		Phase:       "starting",
		Progress:    0.0,
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
	}
	o.mu.Unlock()

	// Run provisioning in background
	go o.runProvisioningWithCleanup(ctx)

	return nil
}

// runProvisioningWithCleanup executes provisioning and cleans up state afterwards
func (o *ProvisioningOrchestrator) runProvisioningWithCleanup(ctx context.Context) {
	startTime := time.Now()

	defer func() {
		// Keep state visible for 30s after completion for monitoring
		time.AfterFunc(30*time.Second, func() {
			o.mu.Lock()
			o.currentProvisioning = nil
			o.mu.Unlock()
		})
	}()

	// Provision via domain service
	provisionedNode, err := o.domainService.ProvisionNode(ctx)

	if err != nil {
		o.mu.Lock()
		if o.currentProvisioning != nil {
			o.currentProvisioning.ErrorMessage = err.Error()
			o.currentProvisioning.IsActive = false
			o.currentProvisioning.LastUpdated = time.Now()
		}
		o.mu.Unlock()

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

// ProgressReporter Interface Implementation
// These methods are called by the domain layer to report progress

// ReportProgress reports provisioning progress from domain layer
func (o *ProvisioningOrchestrator) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.currentProvisioning != nil {
		o.currentProvisioning.NodeID = nodeID
		o.currentProvisioning.Phase = phase
		o.currentProvisioning.Progress = progress
		o.currentProvisioning.LastUpdated = time.Now()
		o.currentProvisioning.EstimatedETA = o.calculateETALocked(progress)

		// Update phase duration tracking
		if progress > 0 {
			elapsed := time.Since(o.currentProvisioning.StartedAt)
			estimatedPhaseTime := time.Duration(float64(elapsed) / progress)
			o.historicalDurations[phase] = estimatedPhaseTime
		}
	}

	// Publish progress event to event bus
	if o.eventBus != nil {
		return o.eventBus.PublishProgress(nodeID, phase, progress, message, metadata)
	}

	return nil
}

// ReportPhaseStart reports the start of a new provisioning phase
func (o *ProvisioningOrchestrator) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	o.logger.Debug("phase started", slog.String("node_id", nodeID), slog.String("phase", phase))
	return nil
}

// ReportPhaseComplete reports the completion of a provisioning phase
func (o *ProvisioningOrchestrator) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	// Calculate duration from start of current provisioning
	var duration time.Duration
	o.mu.RLock()
	if o.currentProvisioning != nil {
		duration = time.Since(o.currentProvisioning.StartedAt)
	}
	o.mu.RUnlock()

	o.updateHistoricalDuration(phase, duration)
	o.logger.Debug("phase completed",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.Duration("duration", duration))
	return nil
}

// ReportError reports a provisioning error
func (o *ProvisioningOrchestrator) ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	o.mu.Lock()
	if o.currentProvisioning != nil {
		o.currentProvisioning.ErrorMessage = errorMsg
		o.currentProvisioning.IsActive = false
		o.currentProvisioning.LastUpdated = time.Now()
	}
	o.mu.Unlock()

	// Publish failure event
	if o.eventBus != nil {
		return o.eventBus.PublishFailed(nodeID, phase, errorMsg, retryable)
	}

	return nil
}

// ReportCompleted reports successful provisioning completion
func (o *ProvisioningOrchestrator) ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	o.mu.Lock()
	if o.currentProvisioning != nil {
		o.currentProvisioning.IsActive = false
		o.currentProvisioning.Progress = 1.0
		o.currentProvisioning.Phase = "completed"
		o.currentProvisioning.LastUpdated = time.Now()
	}
	o.mu.Unlock()

	// Publish completion event
	if o.eventBus != nil {
		return o.eventBus.PublishCompleted(nodeID, serverID, ipAddress, duration)
	}

	return nil
}

// Status Query Methods

// IsProvisioning returns true if provisioning is currently active
func (o *ProvisioningOrchestrator) IsProvisioning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.currentProvisioning != nil && o.currentProvisioning.IsActive
}

// GetCurrentStatus returns the current provisioning status
func (o *ProvisioningOrchestrator) GetCurrentStatus() *ProvisioningState {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.currentProvisioning == nil {
		return &ProvisioningState{IsActive: false}
	}

	// Return copy to prevent race conditions
	status := *o.currentProvisioning
	return &status
}

// GetEstimatedWaitTime returns estimated time remaining for current provisioning
func (o *ProvisioningOrchestrator) GetEstimatedWaitTime() time.Duration {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.currentProvisioning == nil || !o.currentProvisioning.IsActive {
		return 0
	}

	if o.currentProvisioning.EstimatedETA != nil {
		remaining := time.Until(*o.currentProvisioning.EstimatedETA)
		if remaining > 0 {
			return remaining
		}
	}

	return o.config.DefaultProvisioningETA
}

// GetProgress returns current progress (0.0 to 1.0)
func (o *ProvisioningOrchestrator) GetProgress() float64 {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.currentProvisioning == nil {
		return 0.0
	}

	return o.currentProvisioning.Progress
}

// Helper Methods

// setProvisioningState sets the current provisioning state (thread-safe)
func (o *ProvisioningOrchestrator) setProvisioningState(state *ProvisioningState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	state.LastUpdated = time.Now()
	o.currentProvisioning = state
}

// setError sets error state (thread-safe)
func (o *ProvisioningOrchestrator) setError(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.currentProvisioning != nil {
		o.currentProvisioning.ErrorMessage = err.Error()
		o.currentProvisioning.IsActive = false
		o.currentProvisioning.LastUpdated = time.Now()
	}
}

// getStartTime returns provisioning start time (thread-safe)
func (o *ProvisioningOrchestrator) getStartTime() time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.currentProvisioning != nil {
		return o.currentProvisioning.StartedAt
	}

	return time.Now()
}

// calculateETALocked calculates ETA (must be called with lock held)
func (o *ProvisioningOrchestrator) calculateETALocked(currentProgress float64) *time.Time {
	if currentProgress <= 0 || o.currentProvisioning == nil {
		return nil
	}

	totalDuration, exists := o.historicalDurations["total"]
	if !exists {
		totalDuration = o.config.DefaultProvisioningETA
	}

	elapsed := time.Since(o.currentProvisioning.StartedAt)
	estimatedTotal := time.Duration(float64(elapsed) / currentProgress)

	// Use weighted average with historical data
	if totalDuration > 0 {
		estimatedTotal = (estimatedTotal + totalDuration) / 2
	}

	eta := o.currentProvisioning.StartedAt.Add(estimatedTotal)
	return &eta
}

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
