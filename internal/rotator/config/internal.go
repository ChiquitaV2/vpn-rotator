package config

import "time"

// InternalDefaults provides access to hardcoded configuration defaults
// These settings are not user-configurable but can be accessed by internal components
type InternalDefaults struct{}

// NewInternalDefaults returns internal configuration defaults
func NewInternalDefaults() *InternalDefaults {
	return &InternalDefaults{}
}

// NodeServiceDefaults returns hardcoded node service configuration
func (d *InternalDefaults) NodeServiceDefaults() NodeServiceDefaults {
	return NodeServiceDefaults{
		HealthCheckTimeout:   30 * time.Second,
		ProvisioningTimeout:  10 * time.Minute,
		DestructionTimeout:   5 * time.Minute,
		OptimalNodeSelection: "least_loaded",
	}
}

// NodeInteractorDefaults returns hardcoded node interactor configuration
func (d *InternalDefaults) NodeInteractorDefaults() NodeInteractorDefaults {
	return NodeInteractorDefaults{
		SSHTimeout:          30 * time.Second,
		CommandTimeout:      60 * time.Second,
		WireGuardConfigPath: "/etc/wireguard/wg0.conf",
		WireGuardInterface:  "wg0",
		RetryAttempts:       3,
		RetryBackoff:        2 * time.Second,
	}
}

// DatabaseDefaults returns hardcoded database configuration
func (d *InternalDefaults) DatabaseDefaults() DatabaseDefaults {
	return DatabaseDefaults{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 300 * time.Second,
	}
}

// CircuitBreakerDefaults returns hardcoded circuit breaker configuration
func (d *InternalDefaults) CircuitBreakerDefaults() CircuitBreakerDefaults {
	return CircuitBreakerDefaults{
		FailureThreshold: 5,
		ResetTimeout:     60 * time.Second,
		MaxAttempts:      3,
	}
}

// EventSystemDefaults returns hardcoded event system configuration
func (d *InternalDefaults) EventSystemDefaults() EventSystemDefaults {
	return EventSystemDefaults{
		WorkerTimeout:          15 * time.Minute,
		ETAHistoryRetention:    10,
		DefaultProvisioningETA: 3 * time.Minute,
		ProgressUpdateInterval: 5 * time.Second,
		MaxConcurrentJobs:      1,
	}
}

// NetworkDefaults returns hardcoded network configuration
func (d *InternalDefaults) NetworkDefaults() NetworkDefaults {
	return NetworkDefaults{
		BaseNetwork:       "10.8.0.0/16",
		SubnetMask:        24,
		CapacityThreshold: 0.80,
	}
}

// PeerServiceDefaults returns hardcoded peer service optimization configuration
func (d *InternalDefaults) PeerServiceDefaults() PeerServiceDefaults {
	return PeerServiceDefaults{
		EnableIPCache:       true,
		CacheCleanupPeriod:  5 * time.Minute,
		MaxCacheSize:        10000,
		MaxBatchSize:        100,
		BatchTimeout:        30 * time.Second,
		EnableBatchCreation: true,
		EnableObjectPool:    true,
		MaxPoolSize:         1000,
		StrictValidation:    true,
		EnableMetrics:       false,
	}
}

// ProvisioningDefaults returns hardcoded provisioning service configuration
func (d *InternalDefaults) ProvisioningDefaults() ProvisioningDefaults {
	return ProvisioningDefaults{
		ReadinessTimeout:       5 * time.Minute,
		ReadinessCheckInterval: 30 * time.Second,
		CleanupOnFailure:       true,
		DefaultTags:            map[string]string{"service": "vpn-rotator"},
	}
}

// SchedulerDefaults returns hardcoded scheduler configuration
func (d *InternalDefaults) SchedulerDefaults() SchedulerDefaults {
	return SchedulerDefaults{
		CleanupCheckInterval:  5 * time.Minute,
		ActivityCheckInterval: 30 * time.Second,
		HealthCheckRetries:    3,
		HealthCheckBackoff:    5 * time.Second,
	}
}

// === CONVENIENCE METHODS ===

// GetAllDefaults returns all internal configuration defaults in a structured format
func (d *InternalDefaults) GetAllDefaults() AllInternalDefaults {
	return AllInternalDefaults{
		NodeService:    d.NodeServiceDefaults(),
		NodeInteractor: d.NodeInteractorDefaults(),
		Database:       d.DatabaseDefaults(),
		CircuitBreaker: d.CircuitBreakerDefaults(),
		EventSystem:    d.EventSystemDefaults(),
		Network:        d.NetworkDefaults(),
		PeerService:    d.PeerServiceDefaults(),
		Provisioning:   d.ProvisioningDefaults(),
		Scheduler:      d.SchedulerDefaults(),
	}
}

// AllInternalDefaults contains all internal configuration defaults
type AllInternalDefaults struct {
	NodeService    NodeServiceDefaults
	NodeInteractor NodeInteractorDefaults
	Database       DatabaseDefaults
	CircuitBreaker CircuitBreakerDefaults
	EventSystem    EventSystemDefaults
	Network        NetworkDefaults
	PeerService    PeerServiceDefaults
	Provisioning   ProvisioningDefaults
	Scheduler      SchedulerDefaults
}

// === INTERNAL DEFAULT TYPES ===

type NodeServiceDefaults struct {
	HealthCheckTimeout   time.Duration
	ProvisioningTimeout  time.Duration
	DestructionTimeout   time.Duration
	OptimalNodeSelection string
}

type NodeInteractorDefaults struct {
	SSHTimeout          time.Duration
	CommandTimeout      time.Duration
	WireGuardConfigPath string
	WireGuardInterface  string
	RetryAttempts       int
	RetryBackoff        time.Duration
}

type DatabaseDefaults struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type CircuitBreakerDefaults struct {
	FailureThreshold int
	ResetTimeout     time.Duration
	MaxAttempts      int
}

type EventSystemDefaults struct {
	WorkerTimeout          time.Duration
	ETAHistoryRetention    int
	DefaultProvisioningETA time.Duration
	ProgressUpdateInterval time.Duration
	MaxConcurrentJobs      int
}

type NetworkDefaults struct {
	BaseNetwork       string
	SubnetMask        int
	CapacityThreshold float64
}

type PeerServiceDefaults struct {
	EnableIPCache       bool
	CacheCleanupPeriod  time.Duration
	MaxCacheSize        int
	MaxBatchSize        int
	BatchTimeout        time.Duration
	EnableBatchCreation bool
	EnableObjectPool    bool
	MaxPoolSize         int
	StrictValidation    bool
	EnableMetrics       bool
}

type ProvisioningDefaults struct {
	ReadinessTimeout       time.Duration
	ReadinessCheckInterval time.Duration
	CleanupOnFailure       bool
	DefaultTags            map[string]string
}

type SchedulerDefaults struct {
	CleanupCheckInterval  time.Duration
	ActivityCheckInterval time.Duration
	HealthCheckRetries    int
	HealthCheckBackoff    time.Duration
}
