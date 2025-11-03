package ip

import "fmt"

// Sentinel errors for IP allocation
var (
	ErrNoAvailableSubnets = fmt.Errorf("no available subnets in network range")
	ErrSubnetExhausted    = fmt.Errorf("subnet has no available IPs")
	ErrIPAlreadyAllocated = fmt.Errorf("IP address is already allocated")
	ErrInvalidNodeID      = fmt.Errorf("invalid node ID")
	ErrInvalidCIDR        = fmt.Errorf("invalid CIDR notation")
	ErrInvalidIPAddress   = fmt.Errorf("invalid IP address")
	ErrSubnetNotFound     = fmt.Errorf("subnet not found for node")
)

// SubnetAllocationError represents errors during subnet allocation
type SubnetAllocationError struct {
	NodeID string
	Err    error
}

func (e *SubnetAllocationError) Error() string {
	return fmt.Sprintf("subnet allocation failed for node %s: %v", e.NodeID, e.Err)
}

func (e *SubnetAllocationError) Unwrap() error {
	return e.Err
}

// IPAllocationError represents errors during IP allocation
type IPAllocationError struct {
	NodeID string
	IP     string
	Err    error
}

func (e *IPAllocationError) Error() string {
	if e.IP != "" {
		return fmt.Sprintf("IP allocation failed for %s on node %s: %v", e.IP, e.NodeID, e.Err)
	}
	return fmt.Sprintf("IP allocation failed for node %s: %v", e.NodeID, e.Err)
}

func (e *IPAllocationError) Unwrap() error {
	return e.Err
}

// ValidationError represents validation failures
type ValidationError struct {
	Field string
	Value string
	Err   error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s=%s: %v", e.Field, e.Value, e.Err)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// CapacityError represents capacity-related errors
type CapacityError struct {
	NodeID          string
	Available       int
	Required        int
	ThresholdFailed bool
	Err             error
}

func (e *CapacityError) Error() string {
	if e.ThresholdFailed {
		return fmt.Sprintf("capacity threshold exceeded for node %s: available=%d, required=%d",
			e.NodeID, e.Available, e.Required)
	}
	return fmt.Sprintf("insufficient capacity for node %s: available=%d, required=%d: %v",
		e.NodeID, e.Available, e.Required, e.Err)
}

func (e *CapacityError) Unwrap() error {
	return e.Err
}
