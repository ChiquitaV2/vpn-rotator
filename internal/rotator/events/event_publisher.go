// Package events provides domain-specific event publishers and utilities for the
// VPN rotator system. This file defines the unified event publisher and separates
// event publishing concerns into distinct domains.
package events

import (
	"context"
	"log/slog"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// =============================================================================
// UNIFIED EVENT PUBLISHER
// =============================================================================

// EventPublisher is the central hub for all domain-specific event publishing.
type EventPublisher struct {
	bus          events.EventBus
	logger       *applogger.Logger
	Provisioning *ProvisioningEventPublisher
	Node         *NodeEventPublisher
	Peer         *PeerEventPublisher
	WireGuard    *WireGuardEventPublisher
	System       *SystemEventPublisher
	Resource     *ResourceEventPublisher
}

// NewEventPublisher creates a new unified event publisher.
func NewEventPublisher(bus events.EventBus, logger *applogger.Logger) *EventPublisher {
	// The base publisher provides the core Publish/Subscribe functionality.
	basePublisher := NewDomainEventPublisher(bus, logger)
	scopedLogger := logger.WithComponent("event.publisher")

	return &EventPublisher{
		bus:          bus,
		logger:       scopedLogger,
		Provisioning: &ProvisioningEventPublisher{basePublisher},
		Node:         &NodeEventPublisher{basePublisher},
		Peer:         &PeerEventPublisher{basePublisher},
		WireGuard:    &WireGuardEventPublisher{basePublisher},
		System:       &SystemEventPublisher{basePublisher},
		Resource:     &ResourceEventPublisher{basePublisher},
	}
}

// Close closes the underlying event bus.
func (p *EventPublisher) Close() error {
	p.logger.DebugContext(context.Background(), "closing event bus")
	return p.bus.Close()
}

// Health returns the health of the underlying event bus.
func (p *EventPublisher) Health() events.Health {
	return p.bus.Health()
}

// Bus returns the underlying event bus for advanced usage.
func (p *EventPublisher) Bus() events.EventBus {
	return p.bus
}

// =============================================================================
// DOMAIN EVENT PUBLISHER (BASE)
// =============================================================================

// DomainEventPublisher provides a generic, reusable event publishing interface for any domain.
type DomainEventPublisher struct {
	bus    events.EventBus
	utils  *events.EventUtils
	logger *applogger.Logger
}

// NewDomainEventPublisher creates a new domain event publisher.
func NewDomainEventPublisher(bus events.EventBus, logger *applogger.Logger) *DomainEventPublisher {
	return &DomainEventPublisher{
		bus:    bus,
		utils:  events.NewEventUtils(),
		logger: logger,
	}
}

// Publish publishes any event to the bus.
func (p *DomainEventPublisher) Publish(ctx context.Context, event events.Event) error {
	op := p.logger.StartOp(ctx, "publish_event", slog.String("event_type", event.Type()))

	err := p.bus.Publish(ctx, event)
	if err != nil {
		p.logger.ErrorCtx(ctx, "failed to publish event", err)
		op.Fail(err, "failed to publish event")
		return err
	}
	// Note: We don't log "completed" to avoid excessive log spam.
	return nil
}

// PublishWithRetry publishes an event with retry logic for transient failures.
func (p *DomainEventPublisher) PublishWithRetry(ctx context.Context, event events.Event, maxRetries int) error {
	return events.PublishWithRetry(p.bus, ctx, event, maxRetries)
}

// PublishSimple publishes a simple event with just type and metadata.
func (p *DomainEventPublisher) PublishSimple(ctx context.Context, eventType string, metadata map[string]interface{}) error {
	event := events.NewBaseEvent(eventType, metadata)
	return p.Publish(ctx, event)
}

// PublishSimpleWithRetry publishes a simple event with retry logic.
func (p *DomainEventPublisher) PublishSimpleWithRetry(ctx context.Context, eventType string, metadata map[string]interface{}, maxRetries int) error {
	event := events.NewBaseEvent(eventType, metadata)
	return events.PublishWithRetry(p.bus, ctx, event, maxRetries)
}

// Subscribe subscribes to events of a specific type.
func (p *DomainEventPublisher) Subscribe(eventType string, handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.bus.Subscribe(eventType, handler)
}

// CreateTypedHandler provides access to the utility function for creating typed event handlers.
func (p *DomainEventPublisher) CreateTypedHandler() *events.EventUtils {
	return p.utils
}

// =============================================================================
// PROVISIONING EVENTS
// =============================================================================

// ProvisioningEventPublisher handles provisioning-specific events.
type ProvisioningEventPublisher struct {
	*DomainEventPublisher
}

// PublishProvisionRequested publishes a provision requested event.
func (p *ProvisioningEventPublisher) PublishProvisionRequested(ctx context.Context, requestID, source string) error {
	return p.PublishSimple(ctx, EventProvisionRequested, map[string]interface{}{
		"request_id": requestID,
		"source":     source,
	})
}

// PublishProvisionProgress publishes a provisioning progress event.
func (p *ProvisioningEventPublisher) PublishProvisionProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["node_id"] = nodeID
	metadata["phase"] = phase
	metadata["progress"] = progress
	metadata["message"] = message

	return p.PublishSimple(ctx, EventProvisionProgress, metadata)
}

// PublishProvisionCompleted publishes a provisioning completed event.
func (p *ProvisioningEventPublisher) PublishProvisionCompleted(ctx context.Context, nodeID, serverID, ipAddress string, duration time.Duration) error {
	return p.PublishSimple(ctx, EventProvisionCompleted, map[string]interface{}{
		"node_id":     nodeID,
		"server_id":   serverID,
		"ip_address":  ipAddress,
		"duration":    duration.String(),
		"duration_ms": duration.Milliseconds(),
	})
}

// PublishProvisionFailed publishes a provisioning failed event.
func (p *ProvisioningEventPublisher) PublishProvisionFailed(ctx context.Context, nodeID, phase string, err error) error {
	// Unpack the domain error
	_, code, errorMsg, retryable, meta, _, _ := apperrors.UnpackError(err)

	payload := map[string]interface{}{
		"node_id":    nodeID,
		"phase":      phase,
		"error":      errorMsg,
		"retryable":  retryable,
		"error_code": code,
		"error_meta": meta,
	}

	p.logger.ErrorCtx(ctx, "publishing provision failed event", err, slog.String("node_id", nodeID), slog.String("phase", phase))
	return p.PublishSimple(ctx, EventProvisionFailed, payload)
}

// OnProvisionRequested subscribes to provision request events.
func (p *ProvisioningEventPublisher) OnProvisionRequested(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventProvisionRequested, handler)
}

// OnProvisionProgress subscribes to provision progress events.
func (p *ProvisioningEventPublisher) OnProvisionProgress(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventProvisionProgress, handler)
}

// OnProvisionCompleted subscribes to provision completed events.
func (p *ProvisioningEventPublisher) OnProvisionCompleted(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventProvisionCompleted, handler)
}

// OnProvisionFailed subscribes to provision failed events.
func (p *ProvisioningEventPublisher) OnProvisionFailed(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventProvisionFailed, handler)
}

// ReportProgress reports provisioning progress for a specific phase.
func (p *ProvisioningEventPublisher) ReportProgress(ctx context.Context, nodeID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	return p.PublishProvisionProgress(ctx, nodeID, phase, progress, message, metadata)
}

// =============================================================================
// NODE EVENTS
// =============================================================================

// NodeEventPublisher handles node-specific events.
type NodeEventPublisher struct {
	*DomainEventPublisher
}

// PublishNodeStatusChanged publishes a node status changed event.
func (p *NodeEventPublisher) PublishNodeStatusChanged(ctx context.Context, nodeID, previousStatus, newStatus, reason string) error {
	return p.PublishSimple(ctx, EventNodeStatusChanged, map[string]interface{}{
		"node_id":         nodeID,
		"previous_status": previousStatus,
		"new_status":      newStatus,
		"reason":          reason,
	})
}

// PublishNodeCreated publishes a node created event.
func (p *NodeEventPublisher) PublishNodeCreated(ctx context.Context, nodeID, status string) error {
	return p.PublishSimple(ctx, EventNodeCreated, map[string]interface{}{
		"node_id": nodeID,
		"status":  status,
	})
}

// PublishNodeDestroyed publishes a node destroyed event.
func (p *NodeEventPublisher) PublishNodeDestroyed(ctx context.Context, nodeID, serverID, reason string) error {
	return p.PublishSimple(ctx, EventNodeDestroyed, map[string]interface{}{
		"node_id":   nodeID,
		"server_id": serverID,
		"reason":    reason,
	})
}

// OnNodeStatusChanged subscribes to node status changed events.
func (p *NodeEventPublisher) OnNodeStatusChanged(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventNodeStatusChanged, handler)
}

// =============================================================================
// PEER CONNECTION EVENTS
// =============================================================================

// PeerEventPublisher handles peer connection-specific events.
type PeerEventPublisher struct {
	*DomainEventPublisher
}

// PublishPeerConnectRequested publishes a peer connection request event.
func (p *PeerEventPublisher) PublishPeerConnectRequested(ctx context.Context, requestID string, publicKey *string, generateKeys bool) error {
	metadata := map[string]interface{}{
		"request_id":    requestID,
		"generate_keys": generateKeys,
	}
	if publicKey != nil {
		metadata["public_key"] = *publicKey
	}
	return p.PublishSimple(ctx, EventPeerConnectRequested, metadata)
}

// PublishPeerConnectProgress publishes peer connection progress.
func (p *PeerEventPublisher) PublishPeerConnectProgress(ctx context.Context, requestID, peerID, phase string, progress float64, message string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["request_id"] = requestID
	metadata["phase"] = phase
	metadata["progress"] = progress
	metadata["message"] = message
	if peerID != "" {
		metadata["peer_id"] = peerID
	}
	return p.PublishSimple(ctx, EventPeerConnectProgress, metadata)
}

// PublishPeerConnected publishes a successful peer connection event.
func (p *PeerEventPublisher) PublishPeerConnected(ctx context.Context, requestID, peerID, nodeID, publicKey, allocatedIP, serverPublicKey, serverIP string, serverPort int, clientPrivateKey *string, duration time.Duration) error {
	metadata := map[string]interface{}{
		"request_id":        requestID,
		"peer_id":           peerID,
		"node_id":           nodeID,
		"public_key":        publicKey,
		"allocated_ip":      allocatedIP,
		"server_public_key": serverPublicKey,
		"server_ip":         serverIP,
		"server_port":       serverPort,
		"duration":          duration.String(),
		"duration_ms":       duration.Milliseconds(),
	}
	if clientPrivateKey != nil {
		metadata["client_private_key"] = *clientPrivateKey
	}
	return p.PublishSimple(ctx, EventPeerConnected, metadata)
}

// PublishPeerConnectFailed publishes a peer connection failure event.
func (p *PeerEventPublisher) PublishPeerConnectFailed(ctx context.Context, requestID, phase string, err error) error {
	_, code, errorMsg, retryable, meta, _, _ := apperrors.UnpackError(err)

	payload := map[string]interface{}{
		"request_id": requestID,
		"phase":      phase,
		"error":      errorMsg,
		"retryable":  retryable,
		"error_code": code,
		"error_meta": meta,
	}

	p.logger.ErrorCtx(ctx, "publishing peer connect failed event", err, slog.String("request_id", requestID), slog.String("phase", phase))
	return p.PublishSimple(ctx, EventPeerConnectFailed, payload)
}

// PublishPeerDisconnectRequested publishes a peer disconnection request event.
func (p *PeerEventPublisher) PublishPeerDisconnectRequested(ctx context.Context, requestID, peerID string) error {
	return p.PublishSimple(ctx, EventPeerDisconnectRequested, map[string]interface{}{
		"request_id": requestID,
		"peer_id":    peerID,
	})
}

// PublishPeerDisconnected publishes a successful peer disconnection event.
func (p *PeerEventPublisher) PublishPeerDisconnected(ctx context.Context, requestID, peerID, nodeID string, duration time.Duration) error {
	return p.PublishSimple(ctx, EventPeerDisconnected, map[string]interface{}{
		"request_id":  requestID,
		"peer_id":     peerID,
		"node_id":     nodeID,
		"duration":    duration.String(),
		"duration_ms": duration.Milliseconds(),
	})
}

// OnPeerConnectRequested subscribes to peer connection request events.
func (p *PeerEventPublisher) OnPeerConnectRequested(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerConnectRequested, handler)
}

// OnPeerConnectProgress subscribes to peer connection progress events.
func (p *PeerEventPublisher) OnPeerConnectProgress(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerConnectProgress, handler)
}

// OnPeerConnected subscribes to peer connected events.
func (p *PeerEventPublisher) OnPeerConnected(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerConnected, handler)
}

// OnPeerConnectFailed subscribes to peer connection failed events.
func (p *PeerEventPublisher) OnPeerConnectFailed(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerConnectFailed, handler)
}

// OnPeerDisconnectRequested subscribes to peer disconnection request events.
func (p *PeerEventPublisher) OnPeerDisconnectRequested(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerDisconnectRequested, handler)
}

// OnPeerDisconnected subscribes to peer disconnected events.
func (p *PeerEventPublisher) OnPeerDisconnected(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventPeerDisconnected, handler)
}

// =============================================================================
// WIREGUARD EVENTS
// =============================================================================

// WireGuardEventPublisher handles WireGuard-specific events.
type WireGuardEventPublisher struct {
	*DomainEventPublisher
}

// PublishWireGuardPeerAdded publishes a WireGuard peer added event.
func (p *WireGuardEventPublisher) PublishWireGuardPeerAdded(ctx context.Context, nodeID, nodeIP, peerID, publicKey string, allowedIPs []string) error {
	return p.PublishSimple(ctx, EventWireGuardPeerAdded, map[string]interface{}{
		"node_id":     nodeID,
		"node_ip":     nodeIP,
		"peer_id":     peerID,
		"public_key":  publicKey,
		"allowed_ips": allowedIPs,
	})
}

// PublishWireGuardPeerRemoved publishes a WireGuard peer removed event.
func (p *WireGuardEventPublisher) PublishWireGuardPeerRemoved(ctx context.Context, nodeID, nodeIP, peerID, publicKey string) error {
	return p.PublishSimple(ctx, EventWireGuardPeerRemoved, map[string]interface{}{
		"node_id":    nodeID,
		"node_ip":    nodeIP,
		"peer_id":    peerID,
		"public_key": publicKey,
	})
}

// PublishWireGuardConfigSaved publishes a WireGuard config saved event.
func (p *WireGuardEventPublisher) PublishWireGuardConfigSaved(ctx context.Context, nodeID, nodeIP, publicKey, configPath string) error {
	return p.PublishSimple(ctx, EventWireGuardConfigSaved, map[string]interface{}{
		"node_id":     nodeID,
		"node_ip":     nodeIP,
		"public_key":  publicKey,
		"config_path": configPath,
	})
}

// OnWireGuardEvent subscribes to any WireGuard-related event.
func (p *WireGuardEventPublisher) OnWireGuardEvent(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	// This is a helper to subscribe to multiple events.
	return p.Subscribe(EventWireGuardPeerAdded, handler) // Example, should subscribe to all relevant events
}

// =============================================================================
// SYSTEM EVENTS
// =============================================================================

// SystemEventPublisher handles system-level events like health checks and errors.
type SystemEventPublisher struct {
	*DomainEventPublisher
}

// PublishHealthCheck publishes a health check event.
func (p *SystemEventPublisher) PublishHealthCheck(ctx context.Context, component, status, message string, details map[string]interface{}) error {
	metadata := map[string]interface{}{
		"component": component,
		"status":    status,
		"message":   message,
	}
	if details != nil {
		metadata["details"] = details
	}
	return p.PublishSimple(ctx, EventHealthCheck, metadata)
}

// PublishError publishes a generic error event.
func (p *SystemEventPublisher) PublishError(ctx context.Context, component, operation string, err error) error {
	// Unpack the domain error
	_, code, errorMsg, retryable, meta, _, _ := apperrors.UnpackError(err)

	if meta == nil {
		meta = make(map[string]interface{})
	}
	meta["component"] = component
	meta["operation"] = operation
	meta["error"] = errorMsg
	meta["retryable"] = retryable
	meta["error_code"] = code

	p.logger.ErrorCtx(ctx, "publishing system error event", err, slog.String("component", component), slog.String("operation", operation))
	return p.PublishSimple(ctx, EventError, meta)
}

// OnError subscribes to error events.
func (p *SystemEventPublisher) OnError(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	return p.Subscribe(EventError, handler)
}

// OnSystemEvent subscribes to system-level events (health checks, errors).
func (p *SystemEventPublisher) OnSystemEvent(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	// Example, should subscribe to all relevant events
	return p.Subscribe(EventHealthCheck, handler)
}

// =============================================================================
// RESOURCE EVENTS
// =============================================================================

// ResourceEventPublisher handles generic resource lifecycle events.
type ResourceEventPublisher struct {
	*DomainEventPublisher
}

// PublishResourceCreated publishes a generic resource created event.
func (p *ResourceEventPublisher) PublishResourceCreated(ctx context.Context, resourceType, resourceID string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["resource_type"] = resourceType
	metadata["resource_id"] = resourceID
	return p.PublishSimple(ctx, EventResourceCreated, metadata)
}

// PublishResourceUpdated publishes a generic resource updated event.
func (p *ResourceEventPublisher) PublishResourceUpdated(ctx context.Context, resourceType, resourceID string, changes map[string]interface{}) error {
	metadata := map[string]interface{}{
		"resource_type": resourceType,
		"resource_id":   resourceID,
		"changes":       changes,
	}
	return p.PublishSimple(ctx, EventResourceUpdated, metadata)
}

// PublishResourceDeleted publishes a generic resource deleted event.
func (p *ResourceEventPublisher) PublishResourceDeleted(ctx context.Context, resourceType, resourceID, reason string, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["resource_type"] = resourceType
	metadata["resource_id"] = resourceID
	metadata["reason"] = reason
	return p.PublishSimple(ctx, EventResourceDeleted, metadata)
}

// OnResourceEvent subscribes to generic resource lifecycle events.
func (p *ResourceEventPublisher) OnResourceEvent(handler events.EventHandler) (events.UnsubscribeFunc, error) {
	// Example, should subscribe to all relevant events
	return p.Subscribe(EventResourceCreated, handler)
}
