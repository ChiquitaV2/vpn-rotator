package events

import (
	"context"
	"sync"
	"time"

	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
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
	ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error

	// ReportCompleted reports successful completion of provisioning
	ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error
}

// NoOpProgressReporter is a progress reporter that does nothing
// Useful for testing or when progress reporting is not needed
type NoOpProgressReporter struct{}

func (n *NoOpProgressReporter) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	return nil
}

func (n *NoOpProgressReporter) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	return nil
}

func (n *NoOpProgressReporter) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	return nil
}

func (n *NoOpProgressReporter) ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	return nil
}

func (n *NoOpProgressReporter) ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	return nil
}

// NewNoOpProgressReporter creates a new no-op progress reporter
func NewNoOpProgressReporter() ProgressReporter {
	return &NoOpProgressReporter{}
}

// EventBasedProgressReporter implements ProgressReporter using the new event system
type EventBasedProgressReporter struct {
	publisher *ProvisioningEventPublisher
}

// NewEventBasedProgressReporter creates a new event-based progress reporter
func NewEventBasedProgressReporter(publisher *ProvisioningEventPublisher) ProgressReporter {
	return &EventBasedProgressReporter{
		publisher: publisher,
	}
}

func (r *EventBasedProgressReporter) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, progress, message, metadata)
}

func (r *EventBasedProgressReporter) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 0.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	return r.publisher.PublishProvisionProgress(ctx, nodeID, phase, 1.0, message, nil)
}

func (r *EventBasedProgressReporter) ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	return r.publisher.PublishProvisionFailed(ctx, nodeID, phase, errorMsg, retryable)
}

func (r *EventBasedProgressReporter) ReportCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	return r.publisher.PublishProvisionCompleted(ctx, nodeID, serverID, ipAddress, duration)
}

// NodeStateTracker is the centralized state manager for all provisioning operations
// It maintains the single source of truth for provisioning state by consuming events
type NodeStateTracker struct {
	eventPublisher *ProvisioningEventPublisher

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

func NewNodeStateTracker(eventPublisher *ProvisioningEventPublisher) *NodeStateTracker {
	tracker := &NodeStateTracker{
		eventPublisher: eventPublisher,
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
		t.handleProvisionRequested(event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	}

	// Subscribe to provision progress events
	if unsubscribe, err := t.eventPublisher.OnProvisionProgress(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionProgress(event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	}

	// Subscribe to completion events
	if unsubscribe, err := t.eventPublisher.OnProvisionCompleted(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionCompleted(event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	}

	// Subscribe to failure events
	if unsubscribe, err := t.eventPublisher.OnProvisionFailed(func(ctx context.Context, event sharedevents.Event) error {
		t.handleProvisionFailed(event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	}
}

// Event handlers for state updates

func (t *NodeStateTracker) handleProvisionRequested(event sharedevents.Event) {
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
		IsActive:    false,
		Metadata:    metadata,
	}
}

func (t *NodeStateTracker) handleProvisionProgress(event sharedevents.Event) {
	metadata := event.Metadata()
	nodeID, _ := metadata["node_id"].(string)
	phase, _ := metadata["phase"].(string)
	progress, _ := metadata["progress"].(float64)
	message, _ := metadata["message"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.nodeStates[nodeID]
	if !exists {
		// Create new state if it doesn't exist
		state = &ProvisioningNodeState{
			NodeID:    nodeID,
			StartedAt: time.Now(),
			IsActive:  true,
		}
		t.nodeStates[nodeID] = state
	}

	// Update state
	state.Phase = phase
	state.Progress = progress
	state.Message = message
	state.LastUpdated = time.Now()
	state.Metadata = metadata

	// Calculate ETA based on progress
	if progress > 0 && state.IsActive {
		elapsed := time.Since(state.StartedAt)
		estimatedTotal := time.Duration(float64(elapsed) / progress)
		eta := state.StartedAt.Add(estimatedTotal)
		state.EstimatedETA = &eta
	}
}

func (t *NodeStateTracker) handleProvisionCompleted(event sharedevents.Event) {
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
}

func (t *NodeStateTracker) handleProvisionFailed(event sharedevents.Event) {
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
			// Return copy to prevent race conditions
			stateCopy := *state
			return &stateCopy
		}
	}

	return nil
}

// IsProvisioning returns true if any node is currently being provisioned
func (t *NodeStateTracker) IsProvisioning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.nodeStates {
		if state.IsActive {
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
	for _, unsubscribe := range t.unsubscribers {
		unsubscribe()
	}
}
