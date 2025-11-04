package peer

// Status represents the status of a peer
type Status string

const (
	StatusActive       Status = "active"
	StatusDisconnected Status = "disconnected"
	StatusRemoving     Status = "removing"
)

// IsValid checks if a status is valid
func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusDisconnected, StatusRemoving:
		return true
	default:
		return false
	}
}

// String returns the string representation of the status
func (s Status) String() string {
	return string(s)
}

// Pre-computed transition lookup table for O(1) lookups
var validTransitions = map[Status]map[Status]bool{
	StatusActive: {
		StatusDisconnected: true,
		StatusRemoving:     true,
	},
	StatusDisconnected: {
		StatusActive:   true,
		StatusRemoving: true,
	},
	StatusRemoving: {
		// Removing is a terminal state - peer will be deleted
	},
}

// CanTransitionTo checks if a status can transition to another status with O(1) lookup
func (s Status) CanTransitionTo(target Status) bool {
	allowedTargets, exists := validTransitions[s]
	if !exists {
		return false
	}
	return allowedTargets[target]
}
