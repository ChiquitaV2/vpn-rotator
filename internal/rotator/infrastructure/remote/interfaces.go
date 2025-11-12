package remote

import (
	"context"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

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
	node.HealthChecker
	node.WireGuardManager
	CommandExecutor
}
