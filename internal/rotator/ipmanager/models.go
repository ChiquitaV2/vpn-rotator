package ipmanager

import "net"

// Config holds IP manager configuration
type Config struct {
	BaseNetwork string `json:"base_network"` // Default: "10.8.0.0/16"
	SubnetMask  int    `json:"subnet_mask"`  // Default: 24 (for /24 node subnets)
}

// NodeSubnetInfo contains subnet information for a node
type NodeSubnetInfo struct {
	NodeID     string `json:"node_id"`
	SubnetCIDR string `json:"subnet_cidr"`
	GatewayIP  net.IP `json:"gateway_ip"`
	RangeStart net.IP `json:"range_start"`
	RangeEnd   net.IP `json:"range_end"`
}

// SubnetStats returns statistics about subnet usage
type SubnetStats struct {
	NodeID         string  `json:"node_id"`
	SubnetCIDR     string  `json:"subnet_cidr"`
	TotalIPs       int     `json:"total_ips"`
	AllocatedIPs   int     `json:"allocated_ips"`
	AvailableIPs   int     `json:"available_ips"`
	UtilizationPct float64 `json:"utilization_percent"`
}

// SubnetCapacityInfo returns detailed capacity information for a node's subnet
type SubnetCapacityInfo struct {
	NodeID            string  `json:"node_id"`
	SubnetCIDR        string  `json:"subnet_cidr"`
	TotalCapacity     int     `json:"total_capacity"`
	UsedCapacity      int     `json:"used_capacity"`
	AvailableCapacity int     `json:"available_capacity"`
	UtilizationPct    float64 `json:"utilization_percent"`
	NextAvailableIP   net.IP  `json:"next_available_ip"`
	IsNearCapacity    bool    `json:"is_near_capacity"` // >90% utilized
	IsFull            bool    `json:"is_full"`          // 100% utilized
	RecommendAction   string  `json:"recommend_action"` // "none", "monitor", "expand", "migrate"
}
