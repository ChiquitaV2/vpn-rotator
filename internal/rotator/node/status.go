package node

// Status represents the current state of a VPN node
type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusActive       Status = "active"
	StatusUnhealthy    Status = "unhealthy"
	StatusDestroying   Status = "destroying"
)

// IsValid checks if the status is a valid node status
func (s Status) IsValid() bool {
	switch s {
	case StatusProvisioning, StatusActive, StatusUnhealthy, StatusDestroying:
		return true
	default:
		return false
	}
}

// CanTransitionTo checks if the current status can transition to the target status
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusProvisioning:
		return target == StatusActive || target == StatusUnhealthy || target == StatusDestroying
	case StatusActive:
		return target == StatusUnhealthy || target == StatusDestroying
	case StatusUnhealthy:
		return target == StatusActive || target == StatusDestroying
	case StatusDestroying:
		return false // Terminal state
	default:
		return false
	}
}

// String returns the string representation of the status
func (s Status) String() string {
	return string(s)
}

// IsTerminal returns true if the status is a terminal state
func (s Status) IsTerminal() bool {
	return s == StatusDestroying
}

// IsOperational returns true if the node is operational (can handle traffic)
func (s Status) IsOperational() bool {
	return s == StatusActive
}

// RequiresAttention returns true if the status indicates the node needs attention
func (s Status) RequiresAttention() bool {
	return s == StatusUnhealthy || s == StatusDestroying
}
