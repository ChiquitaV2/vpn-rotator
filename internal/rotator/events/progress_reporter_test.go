package events

import (
	"context"
	"testing"
	"time"

	sharedevents "github.com/chiquitav2/vpn-rotator/pkg/events"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventDrivenArchitecture demonstrates the new event-driven architecture
func TestEventDrivenArchitecture(t *testing.T) {
	logger := applogger.NewDevelopment("test") // <-- Changed

	// Create the event system
	bus := sharedevents.NewGookitEventBus(sharedevents.DefaultEventBusConfig(), logger) // <-- Changed
	eventPublisher := NewEventPublisher(bus, logger)                                    // <-- Changed
	defer bus.Close()

	// Create the centralized state tracker
	stateTracker := NewNodeStateTracker(eventPublisher.Provisioning, logger) // <-- Changed
	defer stateTracker.Stop()

	ctx := context.Background()
	nodeID := "test-node-123"

	// Test event-driven workflow
	err := eventPublisher.Provisioning.PublishProvisionRequested(ctx, "request-123", "test-source")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond) // Allow events to process

	err = eventPublisher.Provisioning.PublishProvisionProgress(ctx, nodeID, "installing", 0.5, "Installing packages", nil)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	err = eventPublisher.Provisioning.PublishProvisionCompleted(ctx, nodeID, "server-456", "192.168.1.100", time.Minute*2)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// Verify state tracker received and processed the events
	state := stateTracker.GetNodeState(nodeID)
	require.NotNil(t, state)
	assert.Equal(t, "completed", state.Phase)
	assert.Equal(t, 1.0, state.Progress)
	assert.Equal(t, "server-456", state.ServerID)
	assert.Equal(t, "192.168.1.100", state.IPAddress)
}

// TestNodeStateTracker tests the centralized state management
func TestNodeStateTracker(t *testing.T) {
	logger := applogger.NewDevelopment("test")                                          // <-- Changed
	bus := sharedevents.NewGookitEventBus(sharedevents.DefaultEventBusConfig(), logger) // <-- Changed
	eventPublisher := NewEventPublisher(bus, logger)                                    // <-- Changed
	defer bus.Close()

	stateTracker := NewNodeStateTracker(eventPublisher.Provisioning, logger) // <-- Changed
	defer stateTracker.Stop()

	ctx := context.Background()
	nodeID := "test-node-456"

	// Initially no provisioning
	assert.False(t, stateTracker.IsProvisioning())
	assert.Nil(t, stateTracker.GetActiveNode())

	// Start provisioning
	err := eventPublisher.Provisioning.PublishProvisionProgress(ctx, nodeID, "validation", 0.2, "Validating input", nil)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// Should now be provisioning
	assert.True(t, stateTracker.IsProvisioning())

	activeNode := stateTracker.GetActiveNode()
	require.NotNil(t, activeNode)
	assert.Equal(t, nodeID, activeNode.NodeID)
	assert.Equal(t, "validation", activeNode.Phase)
	assert.Equal(t, 0.2, activeNode.Progress)

	// Complete provisioning
	err = eventPublisher.Provisioning.PublishProvisionCompleted(ctx, nodeID, "server-123", "10.0.0.1", time.Minute)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// Should no longer be active
	assert.False(t, stateTracker.IsProvisioning())

	// But state should still exist
	state := stateTracker.GetNodeState(nodeID)
	require.NotNil(t, state)
	assert.Equal(t, "completed", state.Phase)
	assert.False(t, state.IsActive)
}
