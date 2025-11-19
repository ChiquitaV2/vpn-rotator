package node

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	sharedevents "github.com/chiquitav2/vpn-rotator/pkg/events"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// ProvisioningState represents the complete state of a provisioning operation
type ProvisioningState struct {
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

// ProvisioningStateTracker manages state for all node provisioning operations
// It maintains the single source of truth for provisioning state by consuming events
type ProvisioningStateTracker struct {
	eventPublisher *events.ProvisioningEventPublisher
	logger         *applogger.Logger

	// State management
	mu            sync.RWMutex
	nodeStates    map[string]*ProvisioningState
	unsubscribers []sharedevents.UnsubscribeFunc
}

// NewProvisioningStateTracker creates a new provisioning state tracker
func NewProvisioningStateTracker(eventPublisher *events.ProvisioningEventPublisher, logger *applogger.Logger) *ProvisioningStateTracker {
	tracker := &ProvisioningStateTracker{
		eventPublisher: eventPublisher,
		logger:         logger.WithComponent("node.provisioning.tracker"),
		nodeStates:     make(map[string]*ProvisioningState),
		unsubscribers:  make([]sharedevents.UnsubscribeFunc, 0),
	}

	// Subscribe to relevant events
	tracker.subscribeToEvents()

	return tracker
}

func (t *ProvisioningStateTracker) subscribeToEvents() {
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

func (t *ProvisioningStateTracker) handleProvisionRequested(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Create new provisioning state
	t.nodeStates[requestID] = &ProvisioningState{
		NodeID:      requestID,
		Phase:       "requested",
		Progress:    0.0,
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
		IsActive:    true,
		Metadata:    metadata,
	}
	t.logger.InfoContext(ctx, "Provisioning requested, state created", slog.String("request_id", requestID))
}

func (t *ProvisioningStateTracker) handleProvisionProgress(ctx context.Context, event sharedevents.Event) {
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
			if s.Phase == "requested" && time.Since(s.StartedAt) < 5*time.Minute {
				state = s
				exists = true
				delete(t.nodeStates, reqID)
				break
			}
		}

		if !exists {
			state = &ProvisioningState{
				NodeID:    nodeID,
				StartedAt: time.Now(),
			}
		}
		t.nodeStates[nodeID] = state
	}

	// Update state
	state.NodeID = nodeID
	state.Phase = phase
	state.Progress = progress
	state.Message = message
	state.LastUpdated = time.Now()
	state.Metadata = metadata
	state.IsActive = true

	// Calculate ETA based on progress
	if progress > 0.01 && state.IsActive {
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

func (t *ProvisioningStateTracker) handleProvisionCompleted(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	serverID, _ := metadata["server_id"].(string)
	ipAddress, _ := metadata["ip_address"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		state = &ProvisioningState{
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

func (t *ProvisioningStateTracker) handleProvisionFailed(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	phase, _ := metadata["phase"].(string)
	errorMsg, _ := metadata["error"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		state = &ProvisioningState{
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

// GetProvisioningState returns the complete state for a node
func (t *ProvisioningStateTracker) GetProvisioningState(nodeID string) *ProvisioningState {
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

// GetActiveProvisioning returns the currently active provisioning node (if any)
func (t *ProvisioningStateTracker) GetActiveProvisioning() *ProvisioningState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive {
			t.logger.DebugContext(context.Background(), "Found active node in state tracker",
				slog.String("node_id", state.NodeID),
				slog.String("phase", state.Phase),
				slog.Float64("progress", state.Progress))
			stateCopy := *state
			return &stateCopy
		}
	}

	t.logger.DebugContext(context.Background(), "No active node found in state tracker")
	return nil
}

// IsProvisioning returns true if any node is currently being provisioned
func (t *ProvisioningStateTracker) IsProvisioning() bool {
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
func (t *ProvisioningStateTracker) GetEstimatedWaitTime() time.Duration {
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
func (t *ProvisioningStateTracker) GetProgress() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive {
			return state.Progress
		}
	}

	return 0.0
}

// GetAllProvisioningStates returns all node states
func (t *ProvisioningStateTracker) GetAllProvisioningStates() map[string]*ProvisioningState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return copies to prevent race conditions
	result := make(map[string]*ProvisioningState)
	for nodeID, state := range t.nodeStates {
		stateCopy := *state
		result[nodeID] = &stateCopy
	}

	return result
}

// Stop unsubscribes from all events
func (t *ProvisioningStateTracker) Stop() {
	t.logger.InfoContext(context.Background(), "stopping provisioning state tracker, unsubscribing from events")
	for _, unsubscribe := range t.unsubscribers {
		unsubscribe()
	}
}
