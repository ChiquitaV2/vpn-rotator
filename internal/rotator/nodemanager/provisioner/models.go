package provisioner

import "time"

// NodeStatus defines the status of a provisioned node
type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusPending  NodeStatus = "pending"
	NodeStatusDeleting NodeStatus = "deleting"
	NodeStatusDeleted  NodeStatus = "deleted"
)

// Node represents a provisioned VPN node
type Node struct {
	ID        int64      `json:"id"`
	IP        string     `json:"ip"`
	PublicKey string     `json:"public_key"`
	Status    NodeStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
