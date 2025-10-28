package provisioner

import (
	"context"

	"github.com/chiquitav2/vpn-rotator/internal/shared/models"
)

// Provisioner defines the interface for provisioning and destroying VPN nodes.
type Provisioner interface {
	ProvisionNode(ctx context.Context) (*models.Node, error)
	DestroyNode(ctx context.Context, serverID string) error
}
