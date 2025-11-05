package peer

import (
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
)

// Config holds internal configuration for peer service optimization
// These are hardcoded defaults that should not be user-configurable
type Config struct {
	// Cache settings
	EnableIPCache      bool
	CacheCleanupPeriod time.Duration
	MaxCacheSize       int

	// Batch operation settings
	MaxBatchSize        int
	BatchTimeout        time.Duration
	EnableBatchCreation bool

	// Pool settings
	EnableObjectPool bool
	MaxPoolSize      int

	// Validation settings
	StrictValidation bool
	EnableMetrics    bool
}

// DefaultConfig returns hardcoded peer service configuration from centralized config
// These settings are intentionally not user-configurable to ensure optimal performance
func DefaultConfig() *Config {
	defaults := config.NewInternalDefaults().PeerServiceDefaults()
	return &Config{
		EnableIPCache:       defaults.EnableIPCache,
		CacheCleanupPeriod:  defaults.CacheCleanupPeriod,
		MaxCacheSize:        defaults.MaxCacheSize,
		MaxBatchSize:        defaults.MaxBatchSize,
		BatchTimeout:        defaults.BatchTimeout,
		EnableBatchCreation: defaults.EnableBatchCreation,
		EnableObjectPool:    defaults.EnableObjectPool,
		MaxPoolSize:         defaults.MaxPoolSize,
		StrictValidation:    defaults.StrictValidation,
		EnableMetrics:       defaults.EnableMetrics,
	}
}
