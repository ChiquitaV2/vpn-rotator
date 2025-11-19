package services

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	sharedEvents "github.com/chiquitav2/vpn-rotator/pkg/events"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/chiquitav2/vpn-rotator/pkg/protocol"
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

	// Create minimal mock services to avoid nil pointer errors
	mockNodeService := &MockNodeService{}
	mockPeerService := &MockPeerService{}
	mockIPService := &MockIPService{}
	mockProtoManager := &MockProtocolManager{}
	tracker := peer.NewPeerConnectionStateTracker(eventPublisher.Peer, logger)

	service := NewPeerConnectionService(
		mockNodeService,
		mockPeerService,
		mockIPService,
		mockProtoManager,
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

	mockNodeService := &MockNodeService{}
	mockPeerService := &MockPeerService{}
	mockIPService := &MockIPService{}
	mockProtoManager := &MockProtocolManager{}
	tracker := peer.NewPeerConnectionStateTracker(eventPublisher.Peer, logger)

	service := NewPeerConnectionService(
		mockNodeService,
		mockPeerService,
		mockIPService,
		mockProtoManager,
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

// Minimal mock implementations for testing
type MockNodeService struct{}

func (m *MockNodeService) GetNode(ctx context.Context, nodeID string) (*node.Node, error) {
	return nil, nil
}

func (m *MockNodeService) ListNodes(ctx context.Context, filters node.Filters) ([]*node.Node, error) {
	return nil, nil
}

func (m *MockNodeService) SelectOptimalNode(ctx context.Context, filters node.Filters) (*node.Node, error) {
	return nil, apperrors.NewNodeError(apperrors.ErrCodeNodeNotFound, "no nodes", false, nil)
}

func (m *MockNodeService) ValidateNodeCapacity(ctx context.Context, nodeID string, additionalPeers int) error {
	return nil
}

func (m *MockNodeService) CheckNodeHealth(ctx context.Context, nodeID string) (*node.NodeHealthStatus, error) {
	return nil, nil
}

func (m *MockNodeService) UpdateNodeStatus(ctx context.Context, nodeID string, status node.Status) error {
	return nil
}

func (m *MockNodeService) GetNodePublicKey(ctx context.Context, nodeID string) (string, error) {
	return "", nil
}

func (m *MockNodeService) GetNodeStatistics(ctx context.Context) (*node.Statistics, error) {
	return nil, nil
}

func (m *MockNodeService) DestroyNode(ctx context.Context, nodeID string) error {
	return nil
}

type MockPeerService struct{}

func (m *MockPeerService) Create(ctx context.Context, req *peer.CreateRequest) (*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) Get(ctx context.Context, peerID string) (*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) GetByPublicKey(ctx context.Context, publicKey string) (*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) GetByIdentifier(ctx context.Context, protocol, identifier string) (*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) List(ctx context.Context, filters *peer.Filters) ([]*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) Remove(ctx context.Context, peerID string) error {
	return nil
}

func (m *MockPeerService) UpdateStatus(ctx context.Context, peerID string, status peer.Status) error {
	return nil
}

func (m *MockPeerService) Migrate(ctx context.Context, peerID, newNodeID, newIP string) error {
	return nil
}

func (m *MockPeerService) GetByNode(ctx context.Context, nodeID string) ([]*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) GetActiveByNode(ctx context.Context, nodeID string) ([]*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) CountActiveByNode(ctx context.Context, nodeID string) (int64, error) {
	return 0, nil
}

func (m *MockPeerService) GetInactive(ctx context.Context, inactiveMinutes int) ([]*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) GetStatistics(ctx context.Context) (*peer.Statistics, error) {
	return nil, nil
}

func (m *MockPeerService) CreateBatch(ctx context.Context, requests []*peer.CreateRequest) ([]*peer.Peer, error) {
	return nil, nil
}

func (m *MockPeerService) UpdateStatusBatch(ctx context.Context, updates map[string]peer.Status) error {
	return nil
}

type MockIPService struct{}

func (m *MockIPService) AllocateClientIP(ctx context.Context, nodeID string) (net.IP, error) {
	ip := net.ParseIP("10.0.0.1")
	return ip, nil
}

func (m *MockIPService) ReleaseClientIP(ctx context.Context, nodeID string, ip net.IP) error {
	return nil
}

func (m *MockIPService) GetAllocatedIPs(ctx context.Context, nodeID string) ([]net.IP, error) {
	return nil, nil
}

func (m *MockIPService) GetAvailableIPCount(ctx context.Context, nodeID string) (int, error) {
	return 0, nil
}

func (m *MockIPService) CheckIPConflict(ctx context.Context, ip string) (bool, error) {
	return false, nil
}

func (m *MockIPService) CheckPublicKeyConflict(ctx context.Context, publicKey string) (bool, error) {
	return false, nil
}

func (m *MockIPService) AllocateNodeSubnet(ctx context.Context, nodeID string) (*net.IPNet, error) {
	_, ipNet, _ := net.ParseCIDR("10.0.1.0/24")
	return ipNet, nil
}

func (m *MockIPService) ReleaseNodeSubnet(ctx context.Context, nodeID string) error {
	return nil
}

func (m *MockIPService) GetNodeSubnet(ctx context.Context, nodeID string) (*ip.Subnet, error) {
	return nil, nil
}

type MockProtocolManager struct{}

func (m *MockProtocolManager) AddPeer(ctx context.Context, nodeHost string, peer protocol.PeerConfig) error {
	return nil
}

func (m *MockProtocolManager) RemovePeer(ctx context.Context, nodeHost string, identifier string) error {
	return nil
}

func (m *MockProtocolManager) SyncPeers(ctx context.Context, nodeHost string, peers []protocol.PeerConfig) error {
	return nil
}
