package nodeinteractor

import (
	"context"
	"log/slog"
)

// EventPublisher defines an interface for publishing node interaction events
type EventPublisher interface {
	PublishWireGuardPeerAdded(nodeID, nodeIP, peerID, publicKey string, allowedIPs []string) error
	PublishWireGuardPeerRemoved(nodeID, nodeIP, peerID, publicKey string) error
	PublishWireGuardConfigSaved(nodeID, nodeIP, publicKey, configPath string) error
}

// EventPublishingNodeInteractor wraps a NodeInteractor to publish events for WireGuard operations
type EventPublishingNodeInteractor struct {
	NodeInteractor
	eventPublisher EventPublisher
	logger         *slog.Logger
}

// NewEventPublishingNodeInteractor creates a new event-publishing node interactor
func NewEventPublishingNodeInteractor(
	baseInteractor NodeInteractor,
	eventPublisher EventPublisher,
	logger *slog.Logger,
) NodeInteractor {
	if eventPublisher == nil {
		// If no event publisher provided, just return the base interactor
		return baseInteractor
	}

	return &EventPublishingNodeInteractor{
		NodeInteractor: baseInteractor,
		eventPublisher: eventPublisher,
		logger:         logger,
	}
}

// AddPeer adds a peer and publishes an event
func (e *EventPublishingNodeInteractor) AddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error {
	// Call the base implementation
	err := e.NodeInteractor.AddPeer(ctx, nodeHost, config)
	if err != nil {
		return err
	}

	// Publish event (non-blocking to avoid impacting the operation)
	go func() {
		if publishErr := e.eventPublisher.PublishWireGuardPeerAdded(
			"", // nodeID will be filled by caller if needed
			nodeHost,
			"", // peerID will be filled by caller if needed
			config.PublicKey,
			config.AllowedIPs,
		); publishErr != nil {
			e.logger.Warn("failed to publish WireGuard peer added event",
				slog.String("node_host", nodeHost),
				slog.String("public_key", config.PublicKey[:8]+"..."),
				slog.String("error", publishErr.Error()))
		}
	}()

	return nil
}

// RemovePeer removes a peer and publishes an event
func (e *EventPublishingNodeInteractor) RemovePeer(ctx context.Context, nodeHost string, publicKey string) error {
	// Call the base implementation
	err := e.NodeInteractor.RemovePeer(ctx, nodeHost, publicKey)
	if err != nil {
		return err
	}

	// Publish event (non-blocking)
	go func() {
		if publishErr := e.eventPublisher.PublishWireGuardPeerRemoved(
			"", // nodeID will be filled by caller if needed
			nodeHost,
			"", // peerID will be filled by caller if needed
			publicKey,
		); publishErr != nil {
			e.logger.Warn("failed to publish WireGuard peer removed event",
				slog.String("node_host", nodeHost),
				slog.String("public_key", publicKey[:8]+"..."),
				slog.String("error", publishErr.Error()))
		}
	}()

	return nil
}
