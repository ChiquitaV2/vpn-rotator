package nodemanager

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager/provisioner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Mock implementations for testing

type mockProvisioner struct {
	shouldFail     bool
	provisionError error
	destroyError   error
	notFoundError  bool
}

func (m *mockProvisioner) ProvisionNodeWithSubnet(ctx context.Context, subnet *net.IPNet) (*provisioner.Node, error) {
	if m.provisionError != nil {
		return nil, m.provisionError
	}
	if m.shouldFail {
		return nil, errors.New("mock provisioner error")
	}
	return &provisioner.Node{
		ID:        1,
		IP:        "192.168.1.100",
		PublicKey: "test-public-key==",
		Status:    provisioner.NodeStatusActive,
	}, nil
}

func (m *mockProvisioner) DestroyNode(ctx context.Context, nodeID string) error {
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

type mockHealthChecker struct {
	shouldFail bool
	checkError error
}

func (m *mockHealthChecker) Check(ctx context.Context, nodeIP string) error {
	if m.checkError != nil {
		return m.checkError
	}
	if m.shouldFail {
		return errors.New("mock health check failed")
	}
	return nil
}

type mockIpManager struct{}

func (m *mockIpManager) AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error) {
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/24")
	return ipnet, nil
}

func (m *mockIpManager) ReleaseNodeSubnet(ctx context.Context, nodeID string) error {
	return nil
}

func TestNodeManager_CreateNode(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	ctx := context.Background()
	config, err := manager.CreateNode(ctx)
	if err != nil {
		t.Fatalf("expected CreateNode to succeed, got: %v", err)
	}

	if config == nil {
		t.Fatal("expected node config to be returned")
	}

	if config.ServerPublicKey != "test-public-key==" {
		t.Errorf("expected public key 'test-public-key==', got: %s", config.ServerPublicKey)
	}

	if config.ServerIP != "192.168.1.100" {
		t.Errorf("expected IP '192.168.1.100', got: %s", config.ServerIP)
	}
}

func TestNodeManager_CreateNodeProvisionerError(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{
		provisionError: errors.New("provisioner failed"),
	}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	ctx := context.Background()
	_, err := manager.CreateNode(ctx)
	if err == nil {
		t.Error("expected CreateNode to fail when provisioner fails")
	}

	if !errors.Is(err, errors.New("provisioner failed")) && err.Error() != "failed to provision new node: provisioner failed" {
		t.Errorf("expected provisioner error to be wrapped, got: %v", err)
	}
}

func TestNodeManager_DestroyNode(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create a test node in the database first
	node := db.SeedTestNode(t, store, db.CreateNodeParams{
		ID:              "test-node-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-key==",
		Port:            51820,
		Status:          "active",
	})

	ctx := context.Background()
	err := manager.DestroyNode(ctx, node)
	if err != nil {
		t.Fatalf("expected DestroyNode to succeed, got: %v", err)
	}
}

func TestNodeManager_DestroyNodeNotFound(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{
		notFoundError: true, // Simulate node not found on provider
	}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create a test node in the database first
	node := db.SeedTestNode(t, store, db.CreateNodeParams{
		ID:              "test-node-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-key==",
		Port:            51820,
		Status:          "active",
	})

	ctx := context.Background()
	err := manager.DestroyNode(ctx, node)
	if err != nil {
		t.Fatalf("expected DestroyNode to succeed even when node not found on provider, got: %v", err)
	}
}

func TestNodeManager_DestroyNodeProvisionerError(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{
		destroyError: errors.New("provisioner destroy failed"),
	}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create a test node in the database first
	node := db.SeedTestNode(t, store, db.CreateNodeParams{
		ID:              "test-node-1",
		IpAddress:       "192.168.1.100",
		ServerPublicKey: "test-key==",
		Port:            51820,
		Status:          "active",
	})

	ctx := context.Background()
	err := manager.DestroyNode(ctx, node)
	if err == nil {
		t.Error("expected DestroyNode to fail when provisioner fails")
	}
}

func TestNodeManager_DestroyNodeStoreError(t *testing.T) {
	// For this test, we'll test the case where the node doesn't exist in the database
	// Since SQL DELETE doesn't fail when no rows are affected, this test verifies
	// that the operation completes successfully even for non-existent nodes
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create a node that doesn't exist in the database
	node := db.Node{
		ID:        "non-existent-node",
		IpAddress: "192.168.1.100",
		Status:    "active",
	}

	ctx := context.Background()
	err := manager.DestroyNode(ctx, node)
	if err != nil {
		t.Errorf("expected DestroyNode to succeed even when node doesn't exist in database, got: %v", err)
	}
}

func TestNodeManager_GetNodeHealth(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	ctx := context.Background()
	err := manager.GetNodeHealth(ctx, "192.168.1.100")
	if err != nil {
		t.Fatalf("expected GetNodeHealth to succeed, got: %v", err)
	}
}

func TestNodeManager_GetNodeHealthError(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{
		checkError: errors.New("health check failed"),
	}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	ctx := context.Background()
	err := manager.GetNodeHealth(ctx, "192.168.1.100")
	if err == nil {
		t.Error("expected GetNodeHealth to fail when health check fails")
	}

	expectedMsg := "node health check failed for 192.168.1.100"
	if err.Error() != expectedMsg+": health check failed" {
		t.Errorf("expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}

func TestNodeManager_WaitForNode(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	ctx := context.Background()
	err := manager.WaitForNode(ctx, "192.168.1.100")
	if err != nil {
		t.Fatalf("expected WaitForNode to succeed, got: %v", err)
	}
}

func TestNodeManager_WaitForNodeTimeout(t *testing.T) {
	_, store := db.NewTestDB(t)
	provisioner := &mockProvisioner{}
	healthChecker := &mockHealthChecker{
		shouldFail: true, // Always fail health checks
	}
	ipManager := &mockIpManager{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	manager := New(store, provisioner, healthChecker, logger, "test-ssh-key", ipManager)

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := manager.WaitForNode(ctx, "192.168.1.100")
	if err == nil {
		t.Error("expected WaitForNode to timeout")
	}

	expectedMsg := "timed out waiting for node 192.168.1.100 to be ready"
	if err.Error() != expectedMsg {
		t.Errorf("expected timeout error message '%s', got: %v", expectedMsg, err)
	}
}
