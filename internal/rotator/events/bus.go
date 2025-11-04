package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gookit/event"
)

// EventBusConfig defines configuration for the event bus
type EventBusConfig struct {
	Mode    string        `json:"mode"`
	Timeout time.Duration `json:"timeout"`
}

// ProvisioningEventBus wraps the gookit event manager for provisioning events
type ProvisioningEventBus struct {
	bus    *event.Manager
	config EventBusConfig
	logger *slog.Logger
}

// NewProvisioningEventBus creates a new event bus for provisioning events with default config
func NewProvisioningEventBus(logger *slog.Logger) *ProvisioningEventBus {
	defaultConfig := EventBusConfig{
		Mode:    "simple",
		Timeout: 15 * time.Minute,
	}
	return NewProvisioningEventBusWithConfig(defaultConfig, logger)
}

// NewProvisioningEventBusWithConfig creates a new event bus with custom configuration
func NewProvisioningEventBusWithConfig(config EventBusConfig, logger *slog.Logger) *ProvisioningEventBus {
	bus := event.NewManager("provisioning")

	// Log the configuration mode (gookit/event doesn't expose mode configuration in this version)
	logger.Debug("creating event bus",
		slog.String("mode", config.Mode),
		slog.Duration("timeout", config.Timeout))

	return &ProvisioningEventBus{
		bus:    bus,
		config: config,
		logger: logger,
	}
}

// PublishProvisionRequested publishes a provision requested event
func (eb *ProvisioningEventBus) PublishProvisionRequested(requestID string) error {
	return eb.PublishProvisionRequestedWithSource(requestID, "rotator")
}

// PublishProvisionRequestedWithSource publishes a provision requested event with a custom source
func (eb *ProvisioningEventBus) PublishProvisionRequestedWithSource(requestID, source string) error {
	payload := ProvisionRequestedEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Source:    source,
	}

	eb.logger.Debug("publishing provision requested event",
		slog.String("request_id", requestID),
		slog.String("source", source))

	err, _ := eb.bus.Fire(EventProvisionRequested, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish provision requested event",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish provision requested event: %w", err)
	}

	return nil
}

// PublishProgress publishes a provisioning progress event
func (eb *ProvisioningEventBus) PublishProgress(nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	payload := ProvisionProgressEvent{
		NodeID:    nodeID,
		Phase:     phase,
		Progress:  progress,
		Message:   message,
		Metadata:  metadata,
		Timestamp: time.Now(),
	}

	eb.logger.Debug("publishing provision progress event",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.Float64("progress", progress),
		slog.String("message", message))

	err, _ := eb.bus.Fire(EventProvisionProgress, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish provision progress event",
			slog.String("node_id", nodeID),
			slog.String("phase", phase),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish provision progress event: %w", err)
	}

	return nil
}

// PublishCompleted publishes a provisioning completed event
func (eb *ProvisioningEventBus) PublishCompleted(nodeID, serverID, ipAddress string, duration time.Duration) error {
	payload := ProvisionCompletedEvent{
		NodeID:    nodeID,
		ServerID:  serverID,
		IPAddress: ipAddress,
		Duration:  duration,
		Timestamp: time.Now(),
	}

	eb.logger.Info("publishing provision completed event",
		slog.String("node_id", nodeID),
		slog.String("server_id", serverID),
		slog.String("ip_address", ipAddress),
		slog.Duration("duration", duration))

	err, _ := eb.bus.Fire(EventProvisionCompleted, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish provision completed event",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish provision completed event: %w", err)
	}

	return nil
}

// PublishFailed publishes a provisioning failed event
func (eb *ProvisioningEventBus) PublishFailed(nodeID, phase, errorMsg string, retryable bool) error {
	payload := ProvisionFailedEvent{
		NodeID:    nodeID,
		Phase:     phase,
		Error:     errorMsg,
		Retryable: retryable,
		Timestamp: time.Now(),
	}

	eb.logger.Error("publishing provision failed event",
		slog.String("node_id", nodeID),
		slog.String("phase", phase),
		slog.String("error", errorMsg),
		slog.Bool("retryable", retryable))

	err, _ := eb.bus.Fire(EventProvisionFailed, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish provision failed event",
			slog.String("node_id", nodeID),
			slog.String("phase", phase),
			slog.String("publish_error", err.Error()))
		return fmt.Errorf("failed to publish provision failed event: %w", err)
	}

	return nil
}

// SubscribeToProvisionRequests subscribes to provision request events
func (eb *ProvisioningEventBus) SubscribeToProvisionRequests(listener event.Listener) {
	eb.bus.On(EventProvisionRequested, listener, event.High)
	eb.logger.Debug("subscribed to provision request events")
}

// SubscribeToProvisionProgress subscribes to provision progress events
func (eb *ProvisioningEventBus) SubscribeToProvisionProgress(listener event.Listener) {
	eb.bus.On(EventProvisionProgress, listener, event.Normal)
	eb.logger.Debug("subscribed to provision progress events")
}

// SubscribeToProvisionCompleted subscribes to provision completed events
func (eb *ProvisioningEventBus) SubscribeToProvisionCompleted(listener event.Listener) {
	eb.bus.On(EventProvisionCompleted, listener, event.Normal)
	eb.logger.Debug("subscribed to provision completed events")
}

// SubscribeToProvisionFailed subscribes to provision failed events
func (eb *ProvisioningEventBus) SubscribeToProvisionFailed(listener event.Listener) {
	eb.bus.On(EventProvisionFailed, listener, event.Normal)
	eb.logger.Debug("subscribed to provision failed events")
}

// Close gracefully shuts down the event bus
func (eb *ProvisioningEventBus) Close() error {
	eb.logger.Debug("closing provisioning event bus")
	eb.bus.Clear()
	return nil
}

// Node lifecycle event publishers

// PublishNodeStatusChanged publishes a node status changed event
func (eb *ProvisioningEventBus) PublishNodeStatusChanged(nodeID, previousStatus, newStatus, reason string) error {
	payload := NodeStatusChangedEvent{
		NodeID:         nodeID,
		PreviousStatus: previousStatus,
		NewStatus:      newStatus,
		Reason:         reason,
		Timestamp:      time.Now(),
	}

	eb.logger.Info("publishing node status changed event",
		slog.String("node_id", nodeID),
		slog.String("previous_status", previousStatus),
		slog.String("new_status", newStatus))

	err, _ := eb.bus.Fire(EventNodeStatusChanged, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish node status changed event",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish node status changed event: %w", err)
	}

	return nil
}

// PublishNodeCreated publishes a node created event
func (eb *ProvisioningEventBus) PublishNodeCreated(nodeID, status string) error {
	payload := NodeCreatedEvent{
		NodeID:    nodeID,
		Status:    status,
		Timestamp: time.Now(),
	}

	eb.logger.Info("publishing node created event",
		slog.String("node_id", nodeID),
		slog.String("status", status))

	err, _ := eb.bus.Fire(EventNodeCreated, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish node created event",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish node created event: %w", err)
	}

	return nil
}

// PublishNodeDestroyed publishes a node destroyed event
func (eb *ProvisioningEventBus) PublishNodeDestroyed(nodeID, serverID, reason string) error {
	payload := NodeDestroyedEvent{
		NodeID:    nodeID,
		ServerID:  serverID,
		Reason:    reason,
		Timestamp: time.Now(),
	}

	eb.logger.Info("publishing node destroyed event",
		slog.String("node_id", nodeID),
		slog.String("server_id", serverID))

	err, _ := eb.bus.Fire(EventNodeDestroyed, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish node destroyed event",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish node destroyed event: %w", err)
	}

	return nil
}

// WireGuard event publishers

// PublishWireGuardPeerAdded publishes a WireGuard peer added event
func (eb *ProvisioningEventBus) PublishWireGuardPeerAdded(nodeID, nodeIP, peerID, publicKey string, allowedIPs []string) error {
	payload := WireGuardPeerAddedEvent{
		NodeID:     nodeID,
		NodeIP:     nodeIP,
		PeerID:     peerID,
		PublicKey:  publicKey,
		AllowedIPs: allowedIPs,
		Timestamp:  time.Now(),
	}

	eb.logger.Info("publishing WireGuard peer added event",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerID))

	err, _ := eb.bus.Fire(EventWireGuardPeerAdded, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish WireGuard peer added event",
			slog.String("node_id", nodeID),
			slog.String("peer_id", peerID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish WireGuard peer added event: %w", err)
	}

	return nil
}

// PublishWireGuardPeerRemoved publishes a WireGuard peer removed event
func (eb *ProvisioningEventBus) PublishWireGuardPeerRemoved(nodeID, nodeIP, peerID, publicKey string) error {
	payload := WireGuardPeerRemovedEvent{
		NodeID:    nodeID,
		NodeIP:    nodeIP,
		PeerID:    peerID,
		PublicKey: publicKey,
		Timestamp: time.Now(),
	}

	eb.logger.Info("publishing WireGuard peer removed event",
		slog.String("node_id", nodeID),
		slog.String("peer_id", peerID))

	err, _ := eb.bus.Fire(EventWireGuardPeerRemoved, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish WireGuard peer removed event",
			slog.String("node_id", nodeID),
			slog.String("peer_id", peerID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish WireGuard peer removed event: %w", err)
	}

	return nil
}

// PublishWireGuardConfigSaved publishes a WireGuard config saved event
func (eb *ProvisioningEventBus) PublishWireGuardConfigSaved(nodeID, nodeIP, publicKey, configPath string) error {
	payload := WireGuardConfigSavedEvent{
		NodeID:     nodeID,
		NodeIP:     nodeIP,
		PublicKey:  publicKey,
		ConfigPath: configPath,
		Timestamp:  time.Now(),
	}

	eb.logger.Debug("publishing WireGuard config saved event",
		slog.String("node_id", nodeID))

	err, _ := eb.bus.Fire(EventWireGuardConfigSaved, event.M{"payload": payload})
	if err != nil {
		eb.logger.Error("failed to publish WireGuard config saved event",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to publish WireGuard config saved event: %w", err)
	}

	return nil
}

// ProgressReporter implementation

// ReportProgress reports provisioning progress for a specific phase
func (eb *ProvisioningEventBus) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	return eb.PublishProgress(nodeID, phase, progress, message, metadata)
}

// ReportPhaseStart reports the start of a provisioning phase
func (eb *ProvisioningEventBus) ReportPhaseStart(ctx context.Context, nodeID, phase, message string) error {
	return eb.PublishProgress(nodeID, phase, 0.0, message, nil)
}

// ReportPhaseComplete reports the completion of a provisioning phase
func (eb *ProvisioningEventBus) ReportPhaseComplete(ctx context.Context, nodeID, phase, message string) error {
	return eb.PublishProgress(nodeID, phase, 1.0, message, nil)
}

// ReportError reports an error during provisioning
func (eb *ProvisioningEventBus) ReportError(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	return eb.PublishFailed(nodeID, phase, errorMsg, retryable)
}
