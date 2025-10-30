package rotator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ipmanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/orchestrator"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/scheduler"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Integration test mocks

type integrationMockProvisioner struct {
	shouldFail     bool
	provisionError error
	destroyError   error
	notFoundError  bool
	nodeCounter    int
}

func (m *integrationMockProvisioner) ProvisionNode(ctx context.Context) (*models.Node, error) {
	if m.provisionError != nil {
		return nil, m.provisionError
	}
	if m.shouldFail {
		return nil, errors.New("mock provisioner error")
	}

	m.nodeCounter++
	return &models.Node{
		ID:        int64(m.nodeCounter),
		IP:        fmt.Sprintf("192.168.1.%d", 100+m.nodeCounter),
		PublicKey: fmt.Sprintf("test-public-key-%d==", m.nodeCounter),
		Status:    models.NodeStatusActive,
	}, nil
}

func (m *integrationMockProvisioner) DestroyNode(ctx context.Context, nodeID string) error {
	if m.destroyError != nil {
		return m.destroyError
	}
	if m.notFoundError {
		return hcloud.Error{Code: hcloud.ErrorCodeNotFound}
	}
	if m.shouldFail {
		return errors.New("mock provisioner error")
	}
	return nil
}

type integrationMockHealthChecker struct {
	shouldFail bool
	checkError error
}

func (m *integrationMockHealthChecker) Check(ctx context.Context, nodeIP string) error {
	if m.checkError != nil {
		return m.checkError
	}
	if m.shouldFail {
		return errors.New("mock health check failed")
	}
	return nil
}

// Mock orchestrator for scheduler tests
type mockOrchestratorForScheduler struct {
	shouldRotate         bool
	rotateError          error
	cleanupError         error
	orphanedNodes        []string
	rotateCallCount      int
	getOrphanedCallCount int
	deleteNodeCallCount  int
}

func (m *mockOrchestratorForScheduler) GetNodeConfig(ctx context.Context, nodeID string) (*models.NodeConfig, error) {
	return &models.NodeConfig{}, nil
}

func (m *mockOrchestratorForScheduler) SelectNodeForPeer(ctx context.Context) (string, error) {
	return "mock-node-id", nil
}

func (m *mockOrchestratorForScheduler) GetNodeLoadBalance(ctx context.Context) (map[string]int, error) {
	return map[string]int{"mock-node-id": 0}, nil
}

func (m *mockOrchestratorForScheduler) ShouldRotate(ctx context.Context) (bool, error) {
	return m.shouldRotate, nil
}

func (m *mockOrchestratorForScheduler) RotateNodes(ctx context.Context) error {
	m.rotateCallCount++
	return m.rotateError
}

func (m *mockOrchestratorForScheduler) GetOrphanedNodes(ctx context.Context, age time.Duration) ([]string, error) {
	m.getOrphanedCallCount++
	return m.orphanedNodes, nil
}

func (m *mockOrchestratorForScheduler) DeleteNode(ctx context.Context, serverID string) error {
	m.deleteNodeCallCount++
	return m.cleanupError
}

// TestOrchestratorWithNodeManager tests the orchestrator with a real NodeManager and mocked provisioner
func TestOrchestratorWithNodeManager(t *testing.T) {
	// Set up test database
	_, store := db.NewTestDB(t)

	// Set up mocked provisioner and health checker
	provisioner := &integrationMockProvisioner{}
	healthChecker := &integrationMockHealthChecker{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create IP manager for node manager
	ipManager, err := ipmanager.NewIPManager(store, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create IP manager: %v", err)
	}

	// Create real NodeManager with mocked dependencies
	nodeManager := nodemanager.New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create peer manager
	peerLogger := &logger.Logger{Logger: logger}
	peerManager := peermanager.NewManager(store, peerLogger)

	// Create real Orchestrator with real NodeManager
	orch := orchestrator.New(store, nodeManager, peerManager, ipManager, logger)

	ctx := context.Background()

	// Test SelectNodeForPeer when no active node exists - should provision a new one
	nodeID, err := orch.SelectNodeForPeer(ctx)
	if err != nil {
		t.Fatalf("expected SelectNodeForPeer to succeed, got: %v", err)
	}

	// Get the node config to verify it was created
	config, err := orch.GetNodeConfig(ctx, nodeID)
	if err != nil {
		t.Fatalf("expected GetNodeConfig to succeed, got: %v", err)
	}

	if config == nil {
		t.Fatal("expected node config to be returned")
	}

	if config.ServerPublicKey != "test-public-key-1==" {
		t.Errorf("expected public key 'test-public-key-1==', got: %s", config.ServerPublicKey)
	}

	if config.ServerIP != "192.168.1.101" {
		t.Errorf("expected IP '192.168.1.101', got: %s", config.ServerIP)
	}

	// Verify that the provisioner was called
	if provisioner.nodeCounter != 1 {
		t.Errorf("expected provisioner to be called once, got %d calls", provisioner.nodeCounter)
	}
}

func TestOrchestratorWithNodeManagerProvisionerError(t *testing.T) {
	// Set up test database
	_, store := db.NewTestDB(t)

	// Set up mocked provisioner that fails
	provisioner := &integrationMockProvisioner{
		provisionError: errors.New("provisioner failed"),
	}
	healthChecker := &integrationMockHealthChecker{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create IP manager for node manager
	ipManager, err := ipmanager.NewIPManager(store, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create IP manager: %v", err)
	}

	// Create real NodeManager with mocked dependencies
	nodeManager := nodemanager.New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create peer manager
	peerLogger := &logger.Logger{Logger: logger}
	peerManager := peermanager.NewManager(store, peerLogger)

	// Create real Orchestrator with real NodeManager
	orch := orchestrator.New(store, nodeManager, peerManager, ipManager, logger)

	ctx := context.Background()

	// Test SelectNodeForPeer when provisioner fails
	_, err := orch.SelectNodeForPeer(ctx)
	if err == nil {
		t.Error("expected SelectNodeForPeer to fail when provisioner fails")
	}
}

func TestOrchestratorWithNodeManagerCleanup(t *testing.T) {
	// Set up test database
	_, store := db.NewTestDB(t)

	// Set up mocked provisioner and health checker
	provisioner := &integrationMockProvisioner{}
	healthChecker := &integrationMockHealthChecker{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create IP manager for node manager
	ipManager, err := ipmanager.NewIPManager(store, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create IP manager: %v", err)
	}

	// Create real NodeManager with mocked dependencies
	nodeManager := nodemanager.New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create peer manager
	peerLogger := &logger.Logger{Logger: logger}
	peerManager := peermanager.NewManager(store, peerLogger)

	// Create real Orchestrator with real NodeManager
	orch := orchestrator.New(store, nodeManager, peerManager, ipManager, logger)

	ctx := context.Background()

	// Create a test node and schedule it for destruction
	node := db.SeedTestNode(t, store, db.CreateNodeParams{
		ID:              "test-node-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-key==",
		Port:            51820,
		Status:          "active",
	})

	// Schedule the node for destruction (past time)
	pastTime := time.Now().Add(-1 * time.Hour)
	err := store.ScheduleNodeDestruction(ctx, db.ScheduleNodeDestructionParams{
		ID:        node.ID,
		Version:   node.Version,
		DestroyAt: sql.NullTime{Time: pastTime, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to schedule node destruction: %v", err)
	}

	// Test GetOrphanedNodes - should find the node scheduled for destruction
	orphanedNodes, err := orch.GetOrphanedNodes(ctx, 30*time.Minute)
	if err != nil {
		t.Fatalf("expected GetOrphanedNodes to succeed, got: %v", err)
	}

	if len(orphanedNodes) != 1 {
		t.Errorf("expected 1 orphaned node, got %d", len(orphanedNodes))
	}

	if len(orphanedNodes) > 0 && orphanedNodes[0] != "test-node-1" {
		t.Errorf("expected orphaned node 'test-node-1', got %s", orphanedNodes[0])
	}

	// Test DeleteNode - should destroy the node
	err = orch.DeleteNode(ctx, "test-node-1")
	if err != nil {
		t.Fatalf("expected DeleteNode to succeed, got: %v", err)
	}

	// Verify the node was deleted from the database
	_, err = store.GetNode(ctx, "test-node-1")
	if err == nil {
		t.Error("expected node to be deleted from database")
	}
}

// TestSchedulerManagerWithOrchestrator tests the scheduler manager with real schedulers and mocked orchestrator
func TestSchedulerManagerWithOrchestrator(t *testing.T) {
	// Create mock orchestrator
	mockOrch := &mockOrchestratorForScheduler{
		shouldRotate: true, // Always indicate rotation is needed
	}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create scheduler manager with very short intervals for testing
	schedulerManager := scheduler.NewManager(
		100*time.Millisecond, // rotation interval
		100*time.Millisecond, // cleanup interval
		1*time.Hour,          // cleanup age
		mockOrch,             // rotation manager
		mockOrch,             // cleanup manager
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start the scheduler manager
	err := schedulerManager.Start(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to start successfully, got: %v", err)
	}

	// Verify scheduler is running
	if !schedulerManager.IsRunning() {
		t.Error("expected scheduler manager to be running")
	}

	// Wait for schedulers to run a few times
	time.Sleep(300 * time.Millisecond)

	// Stop the scheduler manager
	err = schedulerManager.Stop(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to stop successfully, got: %v", err)
	}

	// Verify scheduler is not running
	if schedulerManager.IsRunning() {
		t.Error("expected scheduler manager to not be running after stop")
	}

	// Verify that the orchestrator methods were called
	if mockOrch.rotateCallCount == 0 {
		t.Error("expected rotation scheduler to call RotateNodes at least once")
	}

	if mockOrch.getOrphanedCallCount == 0 {
		t.Error("expected cleanup scheduler to call GetOrphanedNodes at least once")
	}
}

func TestSchedulerManagerWithOrchestratorErrors(t *testing.T) {
	// Create mock orchestrator that returns errors
	mockOrch := &mockOrchestratorForScheduler{
		shouldRotate:  true,
		rotateError:   errors.New("rotation failed"),
		cleanupError:  errors.New("cleanup failed"),
		orphanedNodes: []string{"orphaned-node-1"},
	}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create scheduler manager with very short intervals for testing
	schedulerManager := scheduler.NewManager(
		100*time.Millisecond, // rotation interval
		100*time.Millisecond, // cleanup interval
		1*time.Hour,          // cleanup age
		mockOrch,             // rotation manager
		mockOrch,             // cleanup manager
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start the scheduler manager
	err := schedulerManager.Start(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to start successfully, got: %v", err)
	}

	// Wait for schedulers to run and encounter errors
	time.Sleep(300 * time.Millisecond)

	// Stop the scheduler manager
	err = schedulerManager.Stop(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to stop successfully, got: %v", err)
	}

	// Verify that the orchestrator methods were called despite errors
	if mockOrch.rotateCallCount == 0 {
		t.Error("expected rotation scheduler to call RotateNodes at least once")
	}

	if mockOrch.getOrphanedCallCount == 0 {
		t.Error("expected cleanup scheduler to call GetOrphanedNodes at least once")
	}

	if mockOrch.deleteNodeCallCount == 0 {
		t.Error("expected cleanup scheduler to call DeleteNode at least once")
	}
}

func TestSchedulerManagerStartStop(t *testing.T) {
	// Create mock orchestrator
	mockOrch := &mockOrchestratorForScheduler{}

	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create scheduler manager
	schedulerManager := scheduler.NewManager(
		1*time.Hour, // rotation interval
		1*time.Hour, // cleanup interval
		1*time.Hour, // cleanup age
		mockOrch,    // rotation manager
		mockOrch,    // cleanup manager
		logger,
	)

	ctx := context.Background()

	// Test starting scheduler manager
	err := schedulerManager.Start(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to start successfully, got: %v", err)
	}

	// Verify it's running
	if !schedulerManager.IsRunning() {
		t.Error("expected scheduler manager to be running after start")
	}

	// Test starting again - should not error
	err = schedulerManager.Start(ctx)
	if err != nil {
		t.Errorf("expected no error when starting already running scheduler, got: %v", err)
	}

	// Test stopping
	err = schedulerManager.Stop(ctx)
	if err != nil {
		t.Fatalf("expected scheduler manager to stop successfully, got: %v", err)
	}

	// Verify it's not running
	if schedulerManager.IsRunning() {
		t.Error("expected scheduler manager to not be running after stop")
	}

	// Test stopping again - should not error
	err = schedulerManager.Stop(ctx)
	if err != nil {
		t.Errorf("expected no error when stopping already stopped scheduler, got: %v", err)
	}
}
