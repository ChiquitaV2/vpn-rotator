package peer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// PeerConnectionState represents the complete state of a peer connection operation
type PeerConnectionState struct {
	RequestID    string
	PeerID       string
	Phase        string
	Progress     float64 // 0.0 to 1.0
	StartedAt    time.Time
	EstimatedETA *time.Time
	LastUpdated  time.Time
	ErrorMessage string
	IsActive     bool
	Message      string
	Metadata     map[string]interface{}

	// Connection details (populated on success)
	NodeID           string
	PublicKey        string
	AllocatedIP      string
	ServerPublicKey  string
	ServerIP         string
	ServerPort       int
	ClientPrivateKey *string
}

// PeerConnectionStateTracker manages state for all peer connection operations
// It maintains the single source of truth for peer connection state by consuming events
type PeerConnectionStateTracker struct {
	eventPublisher *events.PeerEventPublisher
	logger         *applogger.Logger

	// State management
	mu               sync.RWMutex
	connectionStates map[string]*PeerConnectionState // requestID -> state
	unsubscribers    []sharedevents.UnsubscribeFunc
}

// NewPeerConnectionStateTracker creates a new peer connection state tracker
func NewPeerConnectionStateTracker(eventPublisher *events.PeerEventPublisher, logger *applogger.Logger) *PeerConnectionStateTracker {
	tracker := &PeerConnectionStateTracker{
		eventPublisher:   eventPublisher,
		logger:           logger.WithComponent("peer.state.tracker"),
		connectionStates: make(map[string]*PeerConnectionState),
		unsubscribers:    make([]sharedevents.UnsubscribeFunc, 0),
	}

	// Subscribe to relevant events
	tracker.subscribeToEvents()

	return tracker
}

func (t *PeerConnectionStateTracker) subscribeToEvents() {
	// Subscribe to connection requested events
	if unsubscribe, err := t.eventPublisher.OnPeerConnectRequested(func(ctx context.Context, event sharedevents.Event) error {
		t.handleConnectRequested(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to peer connect requested events", err)
	}

	// Subscribe to connection progress events
	if unsubscribe, err := t.eventPublisher.OnPeerConnectProgress(func(ctx context.Context, event sharedevents.Event) error {
		t.handleConnectProgress(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to peer connect progress events", err)
	}

	// Subscribe to connection completion events
	if unsubscribe, err := t.eventPublisher.OnPeerConnected(func(ctx context.Context, event sharedevents.Event) error {
		t.handleConnected(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to peer connected events", err)
	}

	// Subscribe to connection failure events
	if unsubscribe, err := t.eventPublisher.OnPeerConnectFailed(func(ctx context.Context, event sharedevents.Event) error {
		t.handleConnectFailed(ctx, event)
		return nil
	}); err == nil {
		t.unsubscribers = append(t.unsubscribers, unsubscribe)
	} else {
		t.logger.ErrorCtx(context.Background(), "failed to subscribe to peer connect failed events", err)
	}
}

// Event handlers

func (t *PeerConnectionStateTracker) handleConnectRequested(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	// Create new connection state
	t.connectionStates[requestID] = &PeerConnectionState{
		RequestID:   requestID,
		Phase:       "requested",
		Progress:    0.0,
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
		IsActive:    true,
		Metadata:    metadata,
	}
	t.logger.InfoContext(ctx, "Peer connection requested, state created", slog.String("request_id", requestID))
}

func (t *PeerConnectionStateTracker) handleConnectProgress(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)
	peerID, _ := metadata["peer_id"].(string)
	phase, _ := metadata["phase"].(string)
	progress, _ := metadata["progress"].(float64)
	message, _ := metadata["message"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.connectionStates[requestID]
	if !exists {
		// Create new state if it doesn't exist
		state = &PeerConnectionState{
			RequestID: requestID,
			StartedAt: time.Now(),
		}
		t.connectionStates[requestID] = state
	}

	// Update state
	if peerID != "" {
		state.PeerID = peerID
	}
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

	t.logger.DebugContext(ctx, "Peer connection progress update",
		slog.String("request_id", requestID),
		slog.String("phase", phase),
		slog.Float64("progress", progress))
}

func (t *PeerConnectionStateTracker) handleConnected(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)
	peerID, _ := metadata["peer_id"].(string)
	nodeID, _ := metadata["node_id"].(string)
	publicKey, _ := metadata["public_key"].(string)
	allocatedIP, _ := metadata["allocated_ip"].(string)
	serverPublicKey, _ := metadata["server_public_key"].(string)
	serverIP, _ := metadata["server_ip"].(string)
	serverPort, _ := metadata["server_port"].(int)

	var clientPrivateKey *string
	if key, ok := metadata["client_private_key"].(string); ok {
		clientPrivateKey = &key
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.connectionStates[requestID]
	if !exists {
		state = &PeerConnectionState{
			RequestID: requestID,
			StartedAt: time.Now(),
		}
		t.connectionStates[requestID] = state
	}

	// Mark as completed
	state.Phase = "completed"
	state.Progress = 1.0
	state.IsActive = false
	state.PeerID = peerID
	state.NodeID = nodeID
	state.PublicKey = publicKey
	state.AllocatedIP = allocatedIP
	state.ServerPublicKey = serverPublicKey
	state.ServerIP = serverIP
	state.ServerPort = serverPort
	state.ClientPrivateKey = clientPrivateKey
	state.LastUpdated = time.Now()
	state.Metadata = metadata

	t.logger.InfoContext(ctx, "Peer connection completed",
		slog.String("request_id", requestID),
		slog.String("peer_id", peerID),
		slog.String("node_id", nodeID))
}

func (t *PeerConnectionStateTracker) handleConnectFailed(ctx context.Context, event sharedevents.Event) {
	metadata := event.Metadata()
	requestID, _ := metadata["request_id"].(string)
	phase, _ := metadata["phase"].(string)
	errorMsg, _ := metadata["error"].(string)

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.connectionStates[requestID]
	if !exists {
		state = &PeerConnectionState{
			RequestID: requestID,
			StartedAt: time.Now(),
		}
		t.connectionStates[requestID] = state
	}

	// Mark as failed
	state.Phase = phase
	state.IsActive = false
	state.ErrorMessage = errorMsg
	state.LastUpdated = time.Now()
	state.Metadata = metadata

	t.logger.WarnContext(ctx, "Peer connection failed",
		slog.String("request_id", requestID),
		slog.String("phase", phase),
		slog.String("error", errorMsg))
}

// Public API methods for querying state

// GetConnectionState returns the complete state for a connection request
func (t *PeerConnectionStateTracker) GetConnectionState(requestID string) *PeerConnectionState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.connectionStates[requestID]
	if !exists {
		return nil
	}

	// Return copy to prevent race conditions
	stateCopy := *state
	return &stateCopy
}

// GetActiveConnections returns all currently active peer connection operations
func (t *PeerConnectionStateTracker) GetActiveConnections() []*PeerConnectionState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	activeConnections := make([]*PeerConnectionState, 0)
	for _, state := range t.connectionStates {
		if state.IsActive && state.Phase != "completed" && state.ErrorMessage == "" {
			stateCopy := *state
			activeConnections = append(activeConnections, &stateCopy)
		}
	}

	return activeConnections
}

// IsConnecting returns true if any peer is currently being connected
func (t *PeerConnectionStateTracker) IsConnecting() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, state := range t.connectionStates {
		if state.IsActive && state.Phase != "completed" && state.ErrorMessage == "" && state.Phase != "requested" {
			return true
		}
	}

	return false
}

// GetAllConnections returns all connection states
func (t *PeerConnectionStateTracker) GetAllConnections() map[string]*PeerConnectionState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return copies to prevent race conditions
	result := make(map[string]*PeerConnectionState)
	for requestID, state := range t.connectionStates {
		stateCopy := *state
		result[requestID] = &stateCopy
	}

	return result
}

// Stop unsubscribes from all events
func (t *PeerConnectionStateTracker) Stop() {
	t.logger.InfoContext(context.Background(), "stopping peer connection state tracker, unsubscribing from events")
	for _, unsubscribe := range t.unsubscribers {
		unsubscribe()
	}
}
