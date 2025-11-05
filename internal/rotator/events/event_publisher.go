// Package events provides domain-specific event publishers and utilities for the
// VPN rotator system. This file defines the unified event publisher and separates
// event publishing concerns into distinct domains.
package events

import (
	"context"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/shared/events"
)

// =============================================================================
// UNIFIED EVENT PUBLISHER
// =============================================================================

// EventPublisher is the central hub for all domain-specific event publishing.
// It provides access to specialized publishers for different domains like provisioning,
// nodes, WireGuard, and system events.
type EventPublisher struct {
	bus          events.EventBus
	Provisioning *ProvisioningEventPublisher
	Node         *NodeEventPublisher
	WireGuard    *WireGuardEventPublisher
	System       *SystemEventPublisher
	Resource     *ResourceEventPublisher
}

// NewEventPublisher creates a new unified event publisher.
func NewEventPublisher(bus events.EventBus) *EventPublisher {
	// The base publisher provides the core Publish/Subscribe functionality.
	basePublisher := NewDomainEventPublisher(bus)

	return &EventPublisher{
		bus:          bus,
		Provisioning: &ProvisioningEventPublisher{basePublisher},
		Node:         &NodeEventPublisher{basePublisher},
		WireGuard:    &WireGuardEventPublisher{basePublisher},
		System:       &SystemEventPublisher{basePublisher},
		Resource:     &ResourceEventPublisher{basePublisher},
	}
}

// Close closes the underlying event bus.
func (p *EventPublisher) Close() error {
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
	bus   events.EventBus
	utils *events.EventUtils
}

// NewDomainEventPublisher creates a new domain event publisher.
func NewDomainEventPublisher(bus events.EventBus) *DomainEventPublisher {
	return &DomainEventPublisher{
		bus:   bus,
		utils: events.NewEventUtils(),
	}
}

// Publish publishes any event to the bus.
func (p *DomainEventPublisher) Publish(ctx context.Context, event events.Event) error {
	return p.bus.Publish(ctx, event)
}

// PublishWithRetry publishes an event with retry logic for transient failures.
func (p *DomainEventPublisher) PublishWithRetry(ctx context.Context, event events.Event, maxRetries int) error {
	return events.PublishWithRetry(p.bus, ctx, event, maxRetries)
}

// PublishSimple publishes a simple event with just type and metadata.
func (p *DomainEventPublisher) PublishSimple(ctx context.Context, eventType string, metadata map[string]interface{}) error {
	event := events.NewBaseEvent(eventType, metadata)
	return p.bus.Publish(ctx, event)
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
func (p *ProvisioningEventPublisher) PublishProvisionFailed(ctx context.Context, nodeID, phase, errorMsg string, retryable bool) error {
	return p.PublishSimple(ctx, EventProvisionFailed, map[string]interface{}{
		"node_id":   nodeID,
		"phase":     phase,
		"error":     errorMsg,
		"retryable": retryable,
	})
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
	// You can expand this pattern for other event groups.
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
func (p *SystemEventPublisher) PublishError(ctx context.Context, component, operation, errorMsg string, retryable bool, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["component"] = component
	metadata["operation"] = operation
	metadata["error"] = errorMsg
	metadata["retryable"] = retryable
	return p.PublishSimple(ctx, EventError, metadata)
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
