package events

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisioningEventBus_PublishProvisionRequested(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := NewProvisioningEventBus(logger)

	// Track received events
	var receivedEvent *ProvisionRequestedEvent
	listener := event.ListenerFunc(func(e event.Event) error {
		payload := e.Get("payload")
		if req, ok := payload.(ProvisionRequestedEvent); ok {
			receivedEvent = &req
		}
		return nil
	})

	bus.SubscribeToProvisionRequests(listener)

	// Publish event
	requestID := "test-request-123"
	err := bus.PublishProvisionRequested(requestID)

	require.NoError(t, err)
	require.NotNil(t, receivedEvent)
	assert.Equal(t, requestID, receivedEvent.RequestID)
	assert.Equal(t, "rotator", receivedEvent.Source)
	assert.WithinDuration(t, time.Now(), receivedEvent.Timestamp, time.Second)
}

func TestProvisioningEventBus_PublishProgress(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := NewProvisioningEventBus(logger)

	// Track received events
	var receivedEvent *ProvisionProgressEvent
	listener := event.ListenerFunc(func(e event.Event) error {
		payload := e.Get("payload")
		if prog, ok := payload.(ProvisionProgressEvent); ok {
			receivedEvent = &prog
		}
		return nil
	})

	bus.SubscribeToProvisionProgress(listener)

	// Publish event
	nodeID := "node-123"
	phase := "validation"
	progress := 0.25
	message := "Validating configuration"
	err := bus.PublishProgress(nodeID, phase, progress, message, nil)

	require.NoError(t, err)
	require.NotNil(t, receivedEvent)
	assert.Equal(t, phase, receivedEvent.Phase)
	assert.Equal(t, progress, receivedEvent.Progress)
	assert.Equal(t, message, receivedEvent.Message)
	assert.WithinDuration(t, time.Now(), receivedEvent.Timestamp, time.Second)
}

func TestProvisioningEventBus_PublishCompleted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := NewProvisioningEventBus(logger)

	// Track received events
	var receivedEvent *ProvisionCompletedEvent
	listener := event.ListenerFunc(func(e event.Event) error {
		payload := e.Get("payload")
		if comp, ok := payload.(ProvisionCompletedEvent); ok {
			receivedEvent = &comp
		}
		return nil
	})

	bus.SubscribeToProvisionCompleted(listener)

	// Publish event
	nodeID := "node-123"
	nodeIP := "1.2.3.4"
	publicKey := "pubkey"
	duration := 2 * time.Minute
	err := bus.PublishCompleted(nodeID, nodeIP, publicKey, duration)

	require.NoError(t, err)
	require.NotNil(t, receivedEvent)
	assert.Equal(t, nodeID, receivedEvent.NodeID)
	assert.Equal(t, duration, receivedEvent.Duration)
	assert.WithinDuration(t, time.Now(), receivedEvent.Timestamp, time.Second)
}

func TestProvisioningEventBus_PublishFailed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := NewProvisioningEventBus(logger)

	// Track received events
	var receivedEvent *ProvisionFailedEvent
	listener := event.ListenerFunc(func(e event.Event) error {
		payload := e.Get("payload")
		if fail, ok := payload.(ProvisionFailedEvent); ok {
			receivedEvent = &fail
		}
		return nil
	})

	bus.SubscribeToProvisionFailed(listener)

	// Publish event
	nodeID := "node-123"
	phase := "cloud_provision"
	errorMsg := "failed to create instance"
	err := bus.PublishFailed(nodeID, phase, errorMsg, false)

	require.NoError(t, err)
	require.NotNil(t, receivedEvent)
	assert.Equal(t, phase, receivedEvent.Phase)
	assert.Equal(t, errorMsg, receivedEvent.Error)
	assert.WithinDuration(t, time.Now(), receivedEvent.Timestamp, time.Second)
}

func TestEventPublisher_PublishWithRetry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := NewProvisioningEventBus(logger)
	publisher := NewEventPublisher(bus)

	// Track received events
	var receivedEvent *ProvisionRequestedEvent
	listener := event.ListenerFunc(func(e event.Event) error {
		payload := e.Get("payload")
		if req, ok := payload.(ProvisionRequestedEvent); ok {
			receivedEvent = &req
		}
		return nil
	})

	bus.SubscribeToProvisionRequests(listener)

	// Test successful publish with retry
	payload := ProvisionRequestedEvent{
		RequestID: "retry-test-123",
		Timestamp: time.Now(),
		Source:    "rotator",
	}

	err := publisher.PublishWithRetry(EventProvisionRequested, payload, 2)

	require.NoError(t, err)
	require.NotNil(t, receivedEvent)
	assert.Equal(t, "retry-test-123", receivedEvent.RequestID)
}

func TestExtractPayload(t *testing.T) {
	// Create a mock event with payload
	e := &event.BasicEvent{}
	e.SetName("test")
	e.SetData(event.M{
		"payload": ProvisionRequestedEvent{
			RequestID: "test-123",
			Timestamp: time.Now(),
			Source:    "rotator",
		},
	})

	// Test successful extraction
	payload, err := ExtractPayload[ProvisionRequestedEvent](e)
	require.NoError(t, err)
	assert.Equal(t, "test-123", payload.RequestID)
	assert.Equal(t, "rotator", payload.Source)

	// Test type mismatch
	_, err = ExtractPayload[ProvisionProgressEvent](e)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "payload type mismatch")
}
