package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCentralizedInternalConfiguration(t *testing.T) {
	internal := NewInternalDefaults()

	t.Run("NodeService Defaults", func(t *testing.T) {
		defaults := internal.NodeServiceDefaults()

		assert.Equal(t, 30*time.Second, defaults.HealthCheckTimeout)
		assert.Equal(t, 10*time.Minute, defaults.ProvisioningTimeout)
		assert.Equal(t, 5*time.Minute, defaults.DestructionTimeout)
		assert.Equal(t, "least_loaded", defaults.OptimalNodeSelection)
	})

	t.Run("NodeInteractor Defaults", func(t *testing.T) {
		defaults := internal.NodeInteractorDefaults()

		assert.Equal(t, 30*time.Second, defaults.SSHTimeout)
		assert.Equal(t, 60*time.Second, defaults.CommandTimeout)
		assert.Equal(t, "/etc/wireguard/wg0.conf", defaults.WireGuardConfigPath)
		assert.Equal(t, "wg0", defaults.WireGuardInterface)
		assert.Equal(t, 3, defaults.RetryAttempts)
		assert.Equal(t, 2*time.Second, defaults.RetryBackoff)
	})

	t.Run("Database Defaults", func(t *testing.T) {
		defaults := internal.DatabaseDefaults()

		assert.Equal(t, 25, defaults.MaxOpenConns)
		assert.Equal(t, 5, defaults.MaxIdleConns)
		assert.Equal(t, 300*time.Second, defaults.ConnMaxLifetime)
	})

	t.Run("Circuit Breaker Defaults", func(t *testing.T) {
		defaults := internal.CircuitBreakerDefaults()

		assert.Equal(t, 5, defaults.FailureThreshold)
		assert.Equal(t, 60*time.Second, defaults.ResetTimeout)
		assert.Equal(t, 3, defaults.MaxAttempts)
	})

	t.Run("Event System Defaults", func(t *testing.T) {
		defaults := internal.EventSystemDefaults()

		assert.Equal(t, 15*time.Minute, defaults.WorkerTimeout)
		assert.Equal(t, 10, defaults.ETAHistoryRetention)
		assert.Equal(t, 3*time.Minute, defaults.DefaultProvisioningETA)
		assert.Equal(t, 5*time.Second, defaults.ProgressUpdateInterval)
		assert.Equal(t, 1, defaults.MaxConcurrentJobs)
	})

	t.Run("Network Defaults", func(t *testing.T) {
		defaults := internal.NetworkDefaults()

		assert.Equal(t, "10.8.0.0/16", defaults.BaseNetwork)
		assert.Equal(t, 24, defaults.SubnetMask)
		assert.Equal(t, 0.80, defaults.CapacityThreshold)
	})

	t.Run("Peer Service Defaults", func(t *testing.T) {
		defaults := internal.PeerServiceDefaults()

		assert.True(t, defaults.EnableIPCache)
		assert.Equal(t, 5*time.Minute, defaults.CacheCleanupPeriod)
		assert.Equal(t, 10000, defaults.MaxCacheSize)
		assert.Equal(t, 100, defaults.MaxBatchSize)
		assert.Equal(t, 30*time.Second, defaults.BatchTimeout)
		assert.True(t, defaults.EnableBatchCreation)
		assert.True(t, defaults.EnableObjectPool)
		assert.Equal(t, 1000, defaults.MaxPoolSize)
		assert.True(t, defaults.StrictValidation)
		assert.False(t, defaults.EnableMetrics)
	})

	t.Run("Provisioning Defaults", func(t *testing.T) {
		defaults := internal.ProvisioningDefaults()

		assert.Equal(t, 5*time.Minute, defaults.ReadinessTimeout)
		assert.Equal(t, 30*time.Second, defaults.ReadinessCheckInterval)
		assert.True(t, defaults.CleanupOnFailure)
		assert.NotEmpty(t, defaults.DefaultTags)
		assert.Equal(t, "vpn-rotator", defaults.DefaultTags["service"])
	})

	t.Run("Scheduler Defaults", func(t *testing.T) {
		defaults := internal.SchedulerDefaults()

		assert.Equal(t, 5*time.Minute, defaults.CleanupCheckInterval)
		assert.Equal(t, 30*time.Second, defaults.ActivityCheckInterval)
		assert.Equal(t, 3, defaults.HealthCheckRetries)
		assert.Equal(t, 5*time.Second, defaults.HealthCheckBackoff)
	})
}

func TestGetAllDefaults(t *testing.T) {
	internal := NewInternalDefaults()
	allDefaults := internal.GetAllDefaults()

	// Test that all sections are populated
	assert.NotZero(t, allDefaults.NodeService.HealthCheckTimeout)
	assert.NotZero(t, allDefaults.NodeInteractor.SSHTimeout)
	assert.NotZero(t, allDefaults.Database.MaxOpenConns)
	assert.NotZero(t, allDefaults.CircuitBreaker.FailureThreshold)
	assert.NotZero(t, allDefaults.EventSystem.WorkerTimeout)
	assert.NotEmpty(t, allDefaults.Network.BaseNetwork)
	assert.True(t, allDefaults.PeerService.EnableIPCache)
	assert.NotZero(t, allDefaults.Provisioning.ReadinessTimeout)
	assert.NotZero(t, allDefaults.Scheduler.CleanupCheckInterval)
}

func TestCentralizedConfigurationConsistency(t *testing.T) {
	t.Run("Network Configuration Consistency", func(t *testing.T) {
		// Test that IP package and centralized config are consistent
		internal := NewInternalDefaults()
		centralizedNetwork := internal.NetworkDefaults()

		// Values should match expected VPN network settings
		assert.Equal(t, "10.8.0.0/16", centralizedNetwork.BaseNetwork)
		assert.Equal(t, 24, centralizedNetwork.SubnetMask)
		assert.Equal(t, 0.80, centralizedNetwork.CapacityThreshold)
	})

	t.Run("Peer Service Configuration Consistency", func(t *testing.T) {
		internal := NewInternalDefaults()
		centralizedPeer := internal.PeerServiceDefaults()

		// Values should be optimized for performance
		assert.True(t, centralizedPeer.EnableIPCache, "IP cache should be enabled for performance")
		assert.True(t, centralizedPeer.EnableBatchCreation, "Batch creation should be enabled for efficiency")
		assert.True(t, centralizedPeer.EnableObjectPool, "Object pool should be enabled for memory management")
		assert.True(t, centralizedPeer.StrictValidation, "Strict validation should be enabled for security")
	})

	t.Run("Timeout Configuration Consistency", func(t *testing.T) {
		internal := NewInternalDefaults()

		nodeService := internal.NodeServiceDefaults()
		eventSystem := internal.EventSystemDefaults()

		// Provisioning timeout should be reasonable compared to worker timeout
		assert.True(t, nodeService.ProvisioningTimeout < eventSystem.WorkerTimeout,
			"Node provisioning timeout should be less than event worker timeout")

		// Health check timeout should be much shorter than provisioning timeout
		assert.True(t, nodeService.HealthCheckTimeout < nodeService.ProvisioningTimeout,
			"Health check timeout should be much shorter than provisioning timeout")
	})
}

func TestInternalDefaultsImmutability(t *testing.T) {
	// Test that getting defaults multiple times returns consistent values
	internal1 := NewInternalDefaults()
	internal2 := NewInternalDefaults()

	defaults1 := internal1.NodeServiceDefaults()
	defaults2 := internal2.NodeServiceDefaults()

	assert.Equal(t, defaults1, defaults2, "Internal defaults should be consistent across instances")
}

func BenchmarkInternalDefaults(b *testing.B) {
	internal := NewInternalDefaults()

	b.Run("NodeServiceDefaults", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = internal.NodeServiceDefaults()
		}
	})

	b.Run("GetAllDefaults", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = internal.GetAllDefaults()
		}
	})
}
