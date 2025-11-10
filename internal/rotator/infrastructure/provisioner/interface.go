package provisioner

import (
	"context"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
)

// CloudProvisioner defines the interface for cloud infrastructure provisioning
type CloudProvisioner interface {
	ProvisionNode(ctx context.Context, config node.ProvisioningConfig) (*node.ProvisionedNode, error)
	DestroyNode(ctx context.Context, serverID string) error
	GetNodeStatus(ctx context.Context, serverID string) (*node.CloudNodeStatus, error)
}
