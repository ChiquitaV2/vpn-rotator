package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// ProgressReporter defines an interface for reporting provisioning progress
// This allows the domain layer to report progress without coupling to event bus implementation
type ProgressReporter interface {
	// ReportProgress reports provisioning progress for a specific phase
	ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error

	// ReportPhaseStart reports the start of a provisioning phase
	ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error

	// ReportPhaseComplete reports the completion of a provisioning phase
	ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error

	// ReportError reports an error during provisioning
	ReportError(ctx context.Context, nodeID, phase string, err error) error

	// ReportCompleted reports successful completion of provisioning
	ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error
}

// EventBasedProgressReporter implements ProgressReporter using the new event system
type EventBasedProgressReporter struct {
	publisher *ProvisioningEventPublisher
	logger    *applogger.Logger
}

// NewEventBasedProgressReporter creates a new event-based progress reporter
func NewEventBasedProgressReporter(publisher *ProvisioningEventPublisher, logger *applogger.Logger) ProgressReporter {
	return &EventBasedProgressReporter{
		publisher: publisher,
		logger:    logger.WithComponent("progress.reporter"),
	}
}

func (r *EventBasedProgressReporter) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	r.logger.DebugContext(ctx, "reporting progress",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.Float64("progress", progress),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (r *EventBasedProgressReporter) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	r.logger.InfoContext(ctx, "phase started",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 0.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	r.logger.InfoContext(ctx, "phase complete",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("message", message))
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 1.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportError(ctx context.Context, nodeID, phase string, err error) error {
	r.logger.ErrorCtx(ctx, "reporting provisioning error", err,
		slog.String("node_id", nodeID),
		slog.String("phase", phase))
	return r.publisher.PublishProvisionFailed(ctx, nodeID, phase, err)
}

func (r *EventBasedProgressReporter) ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	r.logger.InfoContext(ctx, "reporting provisioning completed",
		slog.String("node_id", nodeID),
		slog.String("server_id", serverID),
		slog.Duration("duration", duration))
	return r.publisher.PublishProvisionCompleted(ctx, nodeID, serverID, ipAddress, duration)
}

// NodeStateTracker is the centralized state manager for all provisioning operations
// It maintains the single source of truth for provisioning state by consuming events
type NodeStateTracker struct {
	eventPublisher *ProvisioningEventPublisher
	logger         *applogger.Logger

	// State management
	mu            sync.RWMutex
	nodeStates    map[string]*ProvisioningNodeState
	unsubscribers []sharedevents.UnsubscribeFunc
}

// ProvisioningNodeState represents the complete state of a provisioning operation
type ProvisioningNodeState struct {
	NodeID       string
	Phase        string
	Progress     float64 // 0.0 to 1.0
	StartedAt    time.Time
	EstimatedETA *time.Time
	LastUpdated  time.Time
	ErrorMessage string
	IsActive     bool
	ServerID     string
	IPAddress    string
	Message      string
	Metadata     map[string]interface{}
}

func NewNodeStateTracker(eventPublisher *ProvisioningEventPublisher, logger *applogger.Logger) *NodeStateTracker {
	tracker := &NodeStateTracker{
		eventPublisher: eventPublisher,
		logger:         logger.WithComponent("state.tracker"),
		nodeStates:     make(map[string]*ProvisioningNodeState),
		unsubscribers:  make([]sharedevents.UnsubscribeFunc, 0),
	}

	// Subscribe to relevant events
	tracker.subscribeToEvents()

	return tracker
}

func (t *NodeStateTracker) subscribeToEvents() {
	// Subscribe to provision requested events
	if unsubscribe, err := t.eventPublisher.OnProvisionRequested(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionRequested(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to provision requested events", err)
	}

	// Subscribe to provision progress events
	if unsubscribe, err := t.eventPublisher.OnProvisionProgress(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionProgress(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to provision progress events", err)
	}

	// Subscribe to completion events
	if unsubscribe, err := t.eventPublisher.OnProvisionCompleted(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionCompleted(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to provision completed events", err)
	}

	// Subscribe to failure events
	if unsubscribe, err := t.eventPublisher.OnProvisionFailed(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionFailed(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to provision failed events", err)
	}
}

// Event handlers for state updates

func (t *NodeStateTracker) handleProvisionRequested(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Create new provisioning state
	t.nodeStates[requestID] = &ProvisioningNodeState{
		NodeID:      requestID,
		Phase:       "requested",
		Progress:    0.0,
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
		IsActive:    true, // Mark as active immediately on request
		Metadata:    metadata,
	}
	t.logger.InfoContext(ctx, "Provisioning requested, state created", slog.String("request_id", requestID))
}

func (t *NodeStateTracker) handleProvisionProgress(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	phase, _ := metadata["phase"].(string)
	progress, _ := metadata["progress"].(float64)
	message, _ := metadata["message"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		// Upgrade requested state to active provisioning state
		for reqID, s := range t.nodeStates {
			// check for requested state and time proximity
			if s.Phase == "requested" && time.Since(s.StartedAt) < 5*time.Minute {
				state = s
				exists = true
				delete(t.nodeStates, reqID) // Delete old state by requestID
				break
			}
		}

		if !exists {
			// Create new state if it doesn't exist
			state = &ProvisioningNodeState{
				NodeID:    nodeID,
				StartedAt: time.Now(),
			}
		}
		t.nodeStates[nodeID] = state // Re-add with the correct nodeID
	}

	// Update state
	state.NodeID = nodeID // Ensure NodeID is correct
	state.Phase = phase
	state.Progress = progress
	state.Message = message
	state.LastUpdated = time.Now()
	state.Metadata = metadata
	state.IsActive = true // Ensure it's marked active on progress

	// Calculate ETA based on progress
	if progress > 0.01 && state.IsActive { // Avoid division by zero
		elapsed := time.Since(state.StartedAt)
		estimatedTotal := time.Duration(float64(elapsed) / progress)
		eta := state.StartedAt.Add(estimatedTotal)
		state.EstimatedETA = &eta
	}
	t.logger.DebugContext(ctx, "Provisioning progress update",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.Float64("progress", progress))
}

func (t *NodeStateTracker) handleProvisionCompleted(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	serverID, _ := metadata["server_id"].(string)
	ipAddress, _ := metadata["ip_address"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		state = &ProvisioningNodeState{
			NodeID:    nodeID,
			StartedAt: time.Now(),
		}
		t.nodeStates[nodeID] = state
	}

	// Mark as completed
	state.Phase = "completed"
	state.Progress = 1.0
	state.IsActive = false
	state.ServerID = serverID
	state.IPAddress = ipAddress
	state.LastUpdated = time.Now()
	state.Metadata = metadata
	t.logger.InfoContext(ctx, "Provisioning completed",
		slog.String("node_id", nodeID),
		slog.String("server_id", serverID),
		slog.String("ip_address", ipAddress))
}

func (t *NodeStateTracker) handleProvisionFailed(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	phase, _ := metadata["phase"].(string)
	errorMsg, _ := metadata["error"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		state = &ProvisioningNodeState{
			NodeID:    nodeID,
			StartedAt: time.Now(),
		}
		t.nodeStates[nodeID] = state
	}

	// Mark as failed
	state.Phase = phase
	state.IsActive = false
	state.ErrorMessage = errorMsg
	state.LastUpdated = time.Now()
	state.Metadata = metadata
	t.logger.WarnContext(ctx, "Provisioning failed",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("error", errorMsg))
}

// Public API methods for querying state

// GetNodeState returns the complete state for a node
func (t *NodeStateTracker) GetNodeState(nodeID string) *ProvisioningNodeState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		return nil
	}

	// Return copy to prevent race conditions
	stateCopy := *state
	return &stateCopy
}

// GetActiveNode returns the currently active provisioning node (if any)
func (t *NodeStateTracker) GetActiveNode() *ProvisioningNodeState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive {
			t.logger.DebugContext(context.Background(), "Found active node in state tracker",
				slog.String("node_id", state.NodeID),
				slog.String("phase", state.Phase),
				slog.Float64("progress", state.Progress))
			// Return copy to prevent race conditions
			stateCopy := *state
			return &stateCopy
		}
	}

	t.logger.DebugContext(context.Background(), "No active node found in state tracker")
	return nil
}

// IsProvisioning returns true if any node is currently being provisioned
func (t *NodeStateTracker) IsProvisioning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive && state.Phase != "completed" && state.ErrorMessage == "" && state.Phase != "requested" {
			return true
		}
	}

	return false
}

// GetEstimatedWaitTime returns estimated time remaining for active provisioning
func (t *NodeStateTracker) GetEstimatedWaitTime() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive && state.EstimatedETA != nil {
			remaining := time.Until(*state.EstimatedETA)
			if remaining > 0 {
				return remaining
			}
		}
	}

	return 0
}

// GetProgress returns current progress (0.0 to 1.0) for active provisioning
func (t *NodeStateTracker) GetProgress() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive {
			return state.Progress
		}
	}

	return 0.0
}

// GetAllNodes returns all node states
func (t *NodeStateTracker) GetAllNodes() map[string]*ProvisioningNodeState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return copies to prevent race conditions
	result := make(map[string]*ProvisioningNodeState)
	for nodeID, state := range t.nodeStates {
		stateCopy := *state
		result[nodeID] = &stateCopy
	}

	return result
}

func (t *NodeStateTracker) Stop() {
	t.logger.InfoContext(context.Background(), "stopping node state tracker, unsubscribing from events")
	for _, unsubscribe := range t.unsubscribers {
		unsubscribe()
	}
}
