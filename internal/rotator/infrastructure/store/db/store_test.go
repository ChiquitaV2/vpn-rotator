package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	_, store := NewTestDB(t)

	if store == nil {
		t.Fatal("expected store to be non-nil")
	}

	// Verify schema was created
	sqlStore := store.(*SQLStore)
	var tableCount int
	err := sqlStore.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='nodes'").Scan(&tableCount)
	if err != nil {
		t.Fatalf("failed to query schema: %v", err)
	}

	if tableCount != 1 {
		t.Errorf("expected 1 'nodes' table, got %d", tableCount)
	}
}

func TestCreateNode(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	params := CreateNodeParams{
		ID:              "test-server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-public-key-base64==",
		Port:            51820,
		Status:          "provisioning",
	}

	node, err := store.CreateNode(ctx, params)
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Verify returned node
	if node.ID != params.ID {
		t.Errorf("expected ID %s, got %s", params.ID, node.ID)
	}
	if node.IpAddress != params.IpAddress {
		t.Errorf("expected IP %s, got %s", params.IpAddress, node.IpAddress)
	}
	if node.ServerPublicKey != params.ServerPublicKey {
		t.Errorf("expected public key %s, got %s", params.ServerPublicKey, node.ServerPublicKey)
	}
	if node.Status != params.Status {
		t.Errorf("expected status %s, got %s", params.Status, node.Status)
	}
	if node.Version != 1 {
		t.Errorf("expected version 1, got %d", node.Version)
	}
}

func TestGetActiveNode(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Test no active node
	_, err := store.GetActiveNode(ctx)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Create a node
	params := CreateNodeParams{
		ID:              "test-server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-public-key==",
		Port:            51820,
		Status:          "provisioning",
	}
	node, err := store.CreateNode(ctx, params)
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Mark it active
	err = store.MarkNodeActive(ctx, MarkNodeActiveParams{
		ID:      node.ID,
		Version: node.Version,
	})
	if err != nil {
		t.Fatalf("MarkNodeActive failed: %v", err)
	}

	// Get active node
	activeNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("GetActiveNode failed: %v", err)
	}

	if activeNode.ID != node.ID {
		t.Errorf("expected active node ID %s, got %s", node.ID, activeNode.ID)
	}
	if activeNode.Status != "active" {
		t.Errorf("expected status 'active', got %s", activeNode.Status)
	}
}

func TestMarkNodeActive(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create first node
	node1, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "key1==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Mark as active
	err = store.MarkNodeActive(ctx, MarkNodeActiveParams{
		ID:      node1.ID,
		Version: node1.Version,
	})
	if err != nil {
		t.Fatalf("MarkNodeActive failed: %v", err)
	}

	// Verify it's active
	activeNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("GetActiveNode failed: %v", err)
	}

	if activeNode.ID != node1.ID {
		t.Errorf("expected active node %s, got %s", node1.ID, activeNode.ID)
	}

	if activeNode.Status != "active" {
		t.Errorf("expected status 'active', got %s", activeNode.Status)
	}

	// Verify version was incremented
	if activeNode.Version != 2 {
		t.Errorf("expected version 2 after activation, got %d", activeNode.Version)
	}
}

func TestScheduleNodeDestruction(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create and activate a node
	node, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "key==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	err = store.MarkNodeActive(ctx, MarkNodeActiveParams{
		ID:      node.ID,
		Version: node.Version,
	})
	if err != nil {
		t.Fatalf("MarkNodeActive failed: %v", err)
	}

	// Refresh node to get updated version
	updatedNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("GetActiveNode failed: %v", err)
	}

	// Schedule destruction
	destroyAt := time.Now().Add(1 * time.Hour)
	err = store.ScheduleNodeDestruction(ctx, ScheduleNodeDestructionParams{
		ID:        updatedNode.ID,
		Version:   updatedNode.Version,
		DestroyAt: sql.NullTime{Time: destroyAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("ScheduleNodeDestruction failed: %v", err)
	}

	// Verify node is scheduled for destruction
	nodes, err := store.GetNodesForDestruction(ctx)
	if err != nil {
		t.Fatalf("GetNodesForDestruction failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node for destruction, got %d", len(nodes))
	}

	if nodes[0].ID != updatedNode.ID {
		t.Errorf("expected node ID %s, got %s", updatedNode.ID, nodes[0].ID)
	}

	if !nodes[0].DestroyAt.Valid {
		t.Error("expected DestroyAt to be valid")
	}
}

func TestGetNodesDueForRotation(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create and activate a node
	node, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "key==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	err = store.MarkNodeActive(ctx, MarkNodeActiveParams{
		ID:      node.ID,
		Version: node.Version,
	})
	if err != nil {
		t.Fatalf("MarkNodeActive failed: %v", err)
	}

	// Get refreshed node
	activeNode, err := store.GetActiveNode(ctx)
	if err != nil {
		t.Fatalf("GetActiveNode failed: %v", err)
	}

	// Simulate connected clients
	err = store.UpdateNodeActivity(ctx, UpdateNodeActivityParams{
		ID:               activeNode.ID,
		LastHandshakeAt:  sql.NullTime{Time: time.Now(), Valid: true},
		ConnectedClients: 1,
	})
	if err != nil {
		t.Fatalf("UpdateNodeActivity failed: %v", err)
	}

	// Check nodes due for rotation (none should be due for 24 hours)
	nodes, err := store.GetNodesDueForRotation(ctx, sql.NullString{String: "24", Valid: true})
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("GetNodesDueForRotation failed: %v", err)
	}

	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes due for rotation, got %d", len(nodes))
	}

	// Check for immediate rotation (0 hours) - should find the node since it has clients
	nodes, err = store.GetNodesDueForRotation(ctx, sql.NullString{String: "0", Valid: true})
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("GetNodesDueForRotation failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("expected 1 node due for rotation, got %d", len(nodes))
	}
}

func TestDeleteNode(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create a node
	node, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "key==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Delete it
	err = store.DeleteNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}

	// Verify it's gone
	nodes, err := store.GetNodesByStatus(ctx, "provisioning")
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("GetNodesByStatus failed: %v", err)
	}

	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after deletion, got %d", len(nodes))
	}
}

func TestGetNodeCount(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Initially should have 0 nodes
	counts, err := store.GetNodeCount(ctx)
	if err != nil {
		t.Fatalf("GetNodeCount failed: %v", err)
	}

	if counts.Total != 0 {
		t.Errorf("expected 0 total nodes, got %d", counts.Total)
	}

	// Create nodes with different statuses
	_, err = store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "key1==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	node2, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              "server-2",
		IpAddress:       "192.168.1.101",
		ServerPublicKey: "key2==",
		Port:            51820,
		Status:          "provisioning",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	err = store.MarkNodeActive(ctx, MarkNodeActiveParams{
		ID:      node2.ID,
		Version: node2.Version,
	})
	if err != nil {
		t.Fatalf("MarkNodeActive failed: %v", err)
	}

	// Check counts
	counts, err = store.GetNodeCount(ctx)
	if err != nil {
		t.Fatalf("GetNodeCount failed: %v", err)
	}

	if counts.Total != 2 {
		t.Errorf("expected 2 total nodes, got %d", counts.Total)
	}

	if !counts.Provisioning.Valid || counts.Provisioning.Float64 != 1 {
		t.Errorf("expected 1 provisioning node, got %v", counts.Provisioning)
	}

	if !counts.Active.Valid || counts.Active.Float64 != 1 {
		t.Errorf("expected 1 active node, got %v", counts.Active)
	}
}

func TestConcurrentNodeCreation(t *testing.T) {
	db, store := NewTestDB(t)
	ctx := context.Background()

	// Verify schema exists before concurrent operations
	var tableCount int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='nodes'").Scan(&tableCount)
	if err != nil {
		t.Fatalf("failed to verify schema: %v", err)
	}
	if tableCount != 1 {
		t.Fatalf("expected nodes table to exist")
	}

	// Test concurrent creation to verify no race conditions
	done := make(chan bool, 3)
	errors := make(chan error, 3)

	for i := 0; i < 3; i++ {
		go func(id int) {
			defer func() { done <- true }()

			params := CreateNodeParams{
				ID:              fmt.Sprintf("server-%d", id),
				IpAddress:       fmt.Sprintf("192.168.1.%d", 100+id),
				ServerPublicKey: fmt.Sprintf("key-%d==", id),
				Port:            51820,
				Status:          "provisioning",
			}
			_, err := store.CreateNode(ctx, params)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent creation failed: %v", err)
	}

	// Verify all 3 nodes were created
	counts, err := store.GetNodeCount(ctx)
	if err != nil {
		t.Fatalf("GetNodeCount failed: %v", err)
	}

	if counts.Total != 3 {
		t.Errorf("expected 3 nodes created, got %d", counts.Total)
	}
}
