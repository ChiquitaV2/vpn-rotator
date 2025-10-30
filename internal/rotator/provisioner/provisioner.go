package provisioner

import (
	"context"
	"net"
)

// Provisioner defines the interface for provisioning and destroying VPN nodes.
type Provisioner interface {
	ProvisionNodeWithSubnet(ctx context.Context, subnet *net.IPNet) (*Node, error)
	DestroyNode(ctx context.Context, serverID string) error
}
