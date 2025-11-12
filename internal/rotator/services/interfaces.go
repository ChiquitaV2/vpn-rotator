package services

import (
	"context"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

// PeerConnector defines the interface for peer connection operations
type PeerConnector interface {
	// Connection operations
	ConnectPeer(ctx context.Context, req ConnectRequest) (*ConnectResponse, error)
	DisconnectPeer(ctx context.Context, peerID string) error

	// Status and information operations
	GetPeerStatus(ctx context.Context, peerID string) (*PeerStatus, error)
	ListPeers(ctx context.Context, params PeerListParams) (*PeersListResponse, error)
}

// NodeRotator defines the interface for node rotation operations
type NodeRotator interface {
	// Node rotation operations
	RotateNodes(ctx context.Context) error
	MigratePeersFromNode(ctx context.Context, sourceNodeID, targetNodeID string) error

	// Cleanup operations (delegated to ResourceCleanupService)
	CleanupInactiveResources(ctx context.Context) error
	CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error
}

// ResourceCleaner defines the interface for resource cleanup operations
type ResourceCleaner interface {
	// Cleanup operations
	CleanupInactiveResources(ctx context.Context) error
	CleanupInactiveResourcesWithOptions(ctx context.Context, options CleanupOptions) error

	// Detection and reporting
	DetectOrphanedResources(ctx context.Context) (*OrphanedResourcesReport, error)
}

// NodeProvisioner defines the interface for node provisioning operations
type NodeProvisioner interface {
	// Provisioning operations
	ProvisionNodeSync(ctx context.Context) (*node.Node, error)
	ProvisionNodeAsync(ctx context.Context) error
	DestroyNode(ctx context.Context, nodeID string) error

	// Worker lifecycle
	StartWorker(ctx context.Context)

	// Status query operations
	IsProvisioning() bool
	GetCurrentStatus() *events.ProvisioningNodeState
	GetEstimatedWaitTime() time.Duration
	GetProgress() float64
}

// SystemAdministrator defines the interface for administrative operations
type SystemAdministrator interface {
	// System status and monitoring
	GetSystemStatus(ctx context.Context) (*SystemStatus, error)
	GetNodeStatistics(ctx context.Context) (*NodeStatistics, error)
	GetPeerStatistics(ctx context.Context) (*PeerStatsResponse, error)

	// Administrative operations
	ForceNodeRotation(ctx context.Context) error
	ManualCleanup(ctx context.Context, options CleanupOptions) (*CleanupResult, error)

	// Rotation and health monitoring
	GetRotationStatus(ctx context.Context) (*RotationStatus, error)
	ValidateSystemHealth(ctx context.Context) (*HealthReport, error)

	// Resource management
	GetOrphanedResourcesReport(ctx context.Context) (*OrphanedResourcesReport, error)
	GetCapacityReport(ctx context.Context) (*CapacityReport, error)
}

// HealthChecker defines the interface for health check operations
type HealthChecker interface {
	GetHealth(ctx context.Context) (*HealthResponse, error)
}
