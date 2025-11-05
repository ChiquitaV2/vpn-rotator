package events

import (
	"context"
	"testing"
	"time"

	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestPublisher(t *testing.T) *EventPublisher {
	log := logger.New("debug", "text")
	config := sharedevents.DefaultEventBusConfig()
	bus := sharedevents.NewGookitEventBus(config, log.Logger)
	t.Cleanup(func() {
		bus.Close()
	})
	return NewEventPublisher(bus)
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

func TestProvisioningEventPublisher_PublishProvisionProgress(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.Provisioning.OnProvisionProgress(func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	nodeID := "node-123"
	phase := "validation"
	progress := 0.25
	message := "Validating configuration"
	customMetadata := map[string]interface{}{
		"custom_field": "custom_value",
	}

	err = publisher.Provisioning.PublishProvisionProgress(ctx, nodeID, phase, progress, message, customMetadata)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventProvisionProgress, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, nodeID, metadata["node_id"])
	assert.Equal(t, phase, metadata["phase"])
	assert.Equal(t, progress, metadata["progress"])
	assert.Equal(t, message, metadata["message"])
	assert.Equal(t, "custom_value", metadata["custom_field"])
}

func TestNodeEventPublisher_PublishNodeStatusChanged(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.Node.OnNodeStatusChanged(func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	nodeID := "node-456"
	prevStatus := "provisioning"
	newStatus := "active"
	reason := "provisioning completed successfully"

	err = publisher.Node.PublishNodeStatusChanged(ctx, nodeID, prevStatus, newStatus, reason)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventNodeStatusChanged, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, nodeID, metadata["node_id"])
	assert.Equal(t, prevStatus, metadata["previous_status"])
	assert.Equal(t, newStatus, metadata["new_status"])
	assert.Equal(t, reason, metadata["reason"])
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
	errorMsg := "something went wrong"
	retryable := false
	err = publisher.System.PublishError(ctx, component, operation, errorMsg, retryable, nil)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventError, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, component, metadata["component"])
	assert.Equal(t, operation, metadata["operation"])
	assert.Equal(t, errorMsg, metadata["error"])
	assert.Equal(t, retryable, metadata["retryable"])
}

func TestResourceEventPublisher_PublishResourceCreated(t *testing.T) {
	publisher := setupTestPublisher(t)
	ctx := context.Background()
	var receivedEvent sharedevents.Event

	unsubscribe, err := publisher.Resource.Subscribe(EventResourceCreated, func(ctx context.Context, event sharedevents.Event) error {
		receivedEvent = event
		return nil
	})
	require.NoError(t, err)
	defer unsubscribe()

	resourceType := "cache"
	resourceID := "cache-123"
	err = publisher.Resource.PublishResourceCreated(ctx, resourceType, resourceID, nil)
	require.NoError(t, err)

	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventResourceCreated, receivedEvent.Type())
	metadata := receivedEvent.Metadata()
	assert.Equal(t, resourceType, metadata["resource_type"])
	assert.Equal(t, resourceID, metadata["resource_id"])
}

func TestEventPublisher_Health(t *testing.T) {
	publisher := setupTestPublisher(t)

	health := publisher.Health()
	assert.Equal(t, "healthy", health.Status)
	assert.Equal(t, 0, health.Subscribers)

	unsubscribe, err := publisher.Provisioning.OnProvisionRequested(func(ctx context.Context, event sharedevents.Event) error {
		return nil
	})
	require.NoError(t, err)

	health = publisher.Health()
	assert.Equal(t, "healthy", health.Status)
	assert.GreaterOrEqual(t, health.Subscribers, 1)

	err = unsubscribe()
	assert.NoError(t, err)

	err = publisher.Close()
	assert.NoError(t, err)

	health = publisher.Health()
	assert.Equal(t, "unhealthy", health.Status)
	assert.Contains(t, health.Message, "closed")
}
