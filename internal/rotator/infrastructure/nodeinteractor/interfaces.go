package nodeinteractor

import (
	"context"
)

// HealthChecker defines the interface for node health and system information operations.
type HealthChecker interface {
	CheckNodeHealth(ctx context.Context, nodeHost string) (*NodeHealthStatus, error)
	GetNodeSystemInfo(ctx context.Context, nodeHost string) (*NodeSystemInfo, error)
}

// WireGuardManager defines the interface for WireGuard-related operations.
type WireGuardManager interface {
	GetWireGuardStatus(ctx context.Context, nodeHost string) (*WireGuardStatus, error)
	AddPeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error
	RemovePeer(ctx context.Context, nodeHost string, publicKey string) error
	UpdatePeer(ctx context.Context, nodeHost string, config PeerWireGuardConfig) error
	ListPeers(ctx context.Context, nodeHost string) ([]*WireGuardPeerStatus, error)
	SyncPeers(ctx context.Context, nodeHost string, configs []PeerWireGuardConfig) error
	UpdateWireGuardConfig(ctx context.Context, nodeHost string, config WireGuardConfig) error
	RestartWireGuard(ctx context.Context, nodeHost string) error
	SaveWireGuardConfig(ctx context.Context, nodeHost string) error
}

// CommandExecutor defines the interface for executing commands and file operations on a node.
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, nodeHost string, command string) (*CommandResult, error)
	UploadFile(ctx context.Context, nodeHost string, localPath, remotePath string) error
	DownloadFile(ctx context.Context, nodeHost string, remotePath, localPath string) error
}

// NodeInteractor is a composite interface that combines all node operations.
// It is implemented by SSHNodeInteractor and can be used where the full set of
// capabilities is required. For dependencies, prefer the smaller, role-specific
// interfaces (HealthChecker, WireGuardManager, CommandExecutor).
type NodeInteractor interface {
	HealthChecker
	WireGuardManager
	CommandExecutor
}
