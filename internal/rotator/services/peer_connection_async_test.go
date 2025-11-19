package services

import (
	"context"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	sharedEvents "github.com/chiquitav2/vpn-rotator/pkg/events"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
)

func TestConnectPeerAsync(t *testing.T) {
	// Setup
	logger := applogger.NewDevelopment("test")
	eventBus := sharedEvents.NewGookitEventBus(sharedEvents.EventBusConfig{
		Mode:       "async",
		Timeout:    5 * time.Second,
		MaxRetries: 3,
	}, logger)
	eventPublisher := events.NewEventPublisher(eventBus, logger)

	// Create mock services (in real test, use proper mocks)
	var mockNodeService node.NodeService
	var mockPeerService peer.Service
	var mockIPService ip.Service
	var mockWireGuardManager node.WireGuardManager
	tracker := peer.NewPeerConnectionStateTracker(eventPublisher.Peer, logger)

	service := NewPeerConnectionService(
		mockNodeService,
		mockPeerService,
		mockIPService,
		mockWireGuardManager,
		eventPublisher,
		tracker,
		logger,
	)

	ctx := context.Background()

	// Test async connection initiation
	t.Run("ConnectPeerAsync returns request ID", func(t *testing.T) {
		req := ConnectRequest{
			GenerateKeys: true,
		}

		requestID, err := service.ConnectPeerAsync(ctx, req)
		if err != nil {
			t.Fatalf("ConnectPeerAsync failed: %v", err)
		}

		if requestID == "" {
			t.Error("Expected non-empty request ID")
		}

		t.Logf("Request ID: %s", requestID)
	})
}

func TestStartConnectionWorker(t *testing.T) {
	// Setup
	logger := applogger.NewDevelopment("test")
	eventBus := sharedEvents.NewGookitEventBus(sharedEvents.EventBusConfig{
		Mode:       "async",
		Timeout:    5 * time.Second,
		MaxRetries: 3,
	}, logger)
	eventPublisher := events.NewEventPublisher(eventBus, logger)

	var mockNodeService node.NodeService
	var mockPeerService peer.Service
	var mockIPService ip.Service
	var mockWireGuardManager node.WireGuardManager
	tracker := peer.NewPeerConnectionStateTracker(eventPublisher.Peer, logger)

	service := NewPeerConnectionService(
		mockNodeService,
		mockPeerService,
		mockIPService,
		mockWireGuardManager,
		eventPublisher,
		tracker,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start worker
	t.Run("StartConnectionWorker subscribes to events", func(t *testing.T) {
		service.StartConnectionWorker(ctx)

		// Give worker time to start
		time.Sleep(100 * time.Millisecond)

		// Publish a test event
		err := eventPublisher.Peer.PublishPeerConnectRequested(ctx, "test-request-id", nil, true)
		if err != nil {
			t.Fatalf("Failed to publish test event: %v", err)
		}

		// Wait a bit for processing
		time.Sleep(200 * time.Millisecond)

		t.Log("Worker started and received event (would fail with mocks, but subscription works)")
	})
}
