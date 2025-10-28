package rotator

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/orchestrator"
)

// TestEndToEndNodeLifecycle tests the complete node lifecycle from creation to cleanup
func TestEndToEndNodeLifecycle(t *testing.T) {
	// Set up test database
	_, store := db.NewTestDB(t)

	// Set up mocked provisioner and health checker
	provisioner := &integrationMockProvisioner{}
	healthChecker := &integrationMockHealthChecker{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create real NodeManager with mocked dependencies
	nodeManager := nodemanager.New(store, provisioner, healthChecker, logger, "test-ssh-key")

	// Create real Orchestrator with real NodeManager
	orch := orchestrator.New(store, nodeManager, logger)

	ctx := context.Background()

	// Step 1: Test initial node creation
	t.Log("Step 1: Creating initial node")
	config1, err := orch.GetLatestConfig(ctx)
	if err != nil {
		t.Fatalf("failed to get initial config: %v", err)
	}

	if config1 == nil {
		t.Fatal("expected node config to be returned")
	}

	// Verify node is in database and active
	activeNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("failed to get active node: %v", err)
	}

	if activeNode.Status != "active" {
		t.Errorf("expected node status 'active', got %s", activeNode.Status)
	}

	t.Logf("Initial node created: %s (IP: %s)", activeNode.ID, activeNode.IpAddress)

	// Step 2: Test node rotation
	t.Log("Step 2: Testing node rotation")
	err = orch.RotateNodes(ctx)
	if err != nil {
		t.Fatalf("failed to rotate nodes: %v", err)
	}

	// Verify new active node exists
	newActiveNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("failed to get new active node: %v", err)
	}

	if newActiveNode.ID == activeNode.ID {
		t.Error("expected new active node to be different from original")
	}

	t.Logf("New active node after rotation: %s (IP: %s)", newActiveNode.ID, newActiveNode.IpAddress)

	// Verify old node is scheduled for destruction
	nodesForDestruction, err := store.GetNodesForDestruction(ctx)
	if err != nil {
		t.Fatalf("failed to get nodes for destruction: %v", err)
	}

	found := false
	for _, node := range nodesForDestruction {
		if node.ID == activeNode.ID {
			found = true
			if !node.DestroyAt.Valid {
				t.Error("expected old node to have destroy time set")
			}
			t.Logf("Old node scheduled for destruction: %s at %v", node.ID, node.DestroyAt.Time)
			break
		}
	}

	if !found {
		t.Error("expected old node to be scheduled for destruction")
	}

	// Step 3: Test cleanup of scheduled nodes
	t.Log("Step 3: Testing cleanup of scheduled nodes")

	// Manually clean up the old node since it's scheduled for future destruction
	t.Logf("Manually cleaning up old node: %s", activeNode.ID)
	err = orch.DeleteNode(ctx, activeNode.ID)
	if err != nil {
		t.Errorf("failed to delete old node %s: %v", activeNode.ID, err)
	}

	// Step 4: Verify final state
	t.Log("Step 4: Verifying final state")

	// Should still have one active node (the new one)
	finalActiveNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("failed to get final active node: %v", err)
	}

	if finalActiveNode.ID != newActiveNode.ID {
		t.Error("expected final active node to be the rotated node")
	}

	// Get node count to verify cleanup
	nodeCount, err := store.GetNodeCount(ctx)
	if err != nil {
		t.Fatalf("failed to get node count: %v", err)
	}

	t.Logf("Final state - Total nodes: %d, Active: %.0f", nodeCount.Total, nodeCount.Active.Float64)

	if nodeCount.Total != 1 {
		t.Errorf("expected 1 total node after cleanup, got %d", nodeCount.Total)
	}

	if !nodeCount.Active.Valid || nodeCount.Active.Float64 != 1 {
		t.Errorf("expected 1 active node after cleanup, got %v", nodeCount.Active)
	}

	t.Log("End-to-end node lifecycle test completed successfully!")
}
