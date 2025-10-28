package models

import "time"

// NodeStatus defines the status of a VPN node.
type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusPending  NodeStatus = "pending"
	NodeStatusDeleting NodeStatus = "deleting"
	NodeStatusDeleted  NodeStatus = "deleted"
)

// Node represents a VPN node in the database.
type Node struct {
	ID        int64
	IP        string
	PublicKey string
	Status    NodeStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NodeConfig represents the VPN node configuration sent to clients.
type NodeConfig struct {
	Version           string    `json:"version"`
	ServerPublicKey   string    `json:"server_public_key"`
	ServerIP          string    `json:"server_ip"`
	ServerPort        int       `json:"server_port"`
	UpdatedAt         time.Time `json:"updated_at"`
	RotationScheduled bool      `json:"rotation_scheduled"`
}
