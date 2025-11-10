package events

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	applogger "github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestPublisher(t *testing.T) *EventPublisher {
	log := applogger.NewDevelopment("event_publisher_test")
	config := sharedevents.DefaultEventBusConfig()
	bus := sharedevents.NewGookitEventBus(config, log)
	t.Cleanup(func() {
		bus.Close()
	})
	return NewEventPublisher(bus, log) // <-- Changed
}

func TestNewEventPublisher(t *testing.T) {
	publisher := setupTestPublisher(t)
	assert.NotNil(t, publisher)
	assert.NotNil(t, publisher.Provisioning)
	assert.NotNil(t, publisher.Node)
	assert.NotNil(t, publisher.WireGuard)
	assert.NotNil(t, publisher.System)
	assert.NotNil(t, publisher.Resource)

	health := publisher.Health()
	assert.Equal(t, "healthy", health.Status)
}

func TestProvisioningEventPublisher_PublishProvisionRequested(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.Provisioning.OnProvisionRequested(func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	requestID := "test-request-123"
	source := "test-source"
	err = publisher.Provisioning.PublishProvisionRequested(ctx, requestID, source)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventProvisionRequested, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, requestID, metadata["request_id"])
	assert.Equal(t, source, metadata["source"])
	assert.WithinDuration(t, time.Now(), receivedEvent.Timestamp(), time.Second)
}

func TestProvisioningEventPublisher_PublishProvisionFailed(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.Provisioning.OnProvisionFailed(func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	nodeID := "node-123"
	phase := "ssh_connection"
	// Test with a DomainError
	originalErr := apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "connection timed out", true, errors.New("i/o timeout")).
		WithMetadata("host", "1.2.3.4")

	err = publisher.Provisioning.PublishProvisionFailed(ctx, nodeID, phase, originalErr)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventProvisionFailed, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, nodeID, metadata["node_id"])
	assert.Equal(t, phase, metadata["phase"])
	assert.Contains(t, "connection timed out", metadata["error"]) // Just the message
	assert.Equal(t, true, metadata["retryable"])
	assert.Equal(t, apperrors.ErrCodeSSHConnection, metadata["error_code"])

	// Check that metadata was unpacked
	errMeta, ok := metadata["error_meta"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1.2.3.4", errMeta["host"])
}

func TestWireGuardEventPublisher_PublishWireGuardPeerAdded(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.WireGuard.Subscribe(EventWireGuardPeerAdded, func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	nodeID := "node-789"
	nodeIP := "10.0.0.1"
	peerID := "peer-1"
	publicKey := "some-public-key"
	allowedIPs := []string{"10.0.0.2/32"}

	err = publisher.WireGuard.PublishWireGuardPeerAdded(ctx, nodeID, nodeIP, peerID, publicKey, allowedIPs)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventWireGuardPeerAdded, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, nodeID, metadata["node_id"])
	assert.Equal(t, nodeIP, metadata["node_ip"])
	assert.Equal(t, peerID, metadata["peer_id"])
	assert.Equal(t, publicKey, metadata["public_key"])
	assert.Equal(t, allowedIPs, metadata["allowed_ips"])
}

func TestSystemEventPublisher_PublishError(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.System.OnError(func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	component := "test-component"
	operation := "test-operation"
	originalErr := apperrors.NewSystemError(apperrors.ErrCodeInternal, "something went wrong", false, nil)

	err = publisher.System.PublishError(ctx, component, operation, originalErr)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventError, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, component, metadata["component"])
	assert.Equal(t, operation, metadata["operation"])
	assert.Equal(t, "something went wrong", metadata["error"])
	assert.Equal(t, false, metadata["retryable"])
	assert.Equal(t, apperrors.ErrCodeInternal, metadata["error_code"])
}
