package peer

import "time"

// Config holds configuration for peer service optimization
type Config struct {
	// Cache settings
	EnableIPCache      bool          `yaml:"enable_ip_cache" json:"enable_ip_cache"`
	CacheCleanupPeriod time.Duration `yaml:"cache_cleanup_period" json:"cache_cleanup_period"`
	MaxCacheSize       int           `yaml:"max_cache_size" json:"max_cache_size"`

	// Batch operation settings
	MaxBatchSize        int           `yaml:"max_batch_size" json:"max_batch_size"`
	BatchTimeout        time.Duration `yaml:"batch_timeout" json:"batch_timeout"`
	EnableBatchCreation bool          `yaml:"enable_batch_creation" json:"enable_batch_creation"`

	// Pool settings
	EnableObjectPool bool `yaml:"enable_object_pool" json:"enable_object_pool"`
	MaxPoolSize      int  `yaml:"max_pool_size" json:"max_pool_size"`

	// Validation settings
	StrictValidation bool `yaml:"strict_validation" json:"strict_validation"`
	EnableMetrics    bool `yaml:"enable_metrics" json:"enable_metrics"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
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

// OptimizedService creates a service with optimizations based on config
func OptimizedService(repo Repository, config *Config, metrics Metrics) Service {
	service := NewService(repo)

	if config.EnableMetrics && metrics != nil {
		service = NewMetricsService(service, metrics)
	}

	return service
}
