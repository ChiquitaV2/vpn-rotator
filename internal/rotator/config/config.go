package config

import "time"

// Config defines the configuration for the rotator service.

type Config struct {
	Service   ServiceConfig   `mapstructure:"service"`
	Log       LogConfig       `mapstructure:"log"`
	API       APIConfig       `mapstructure:"api"`
	DB        DBConfig        `mapstructure:"db"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Hetzner   HetznerConfig   `mapstructure:"hetzner"`
}

// LogConfig defines the logging configuration.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// APIConfig defines the API server configuration.
type APIConfig struct {
	ListenAddr string `mapstructure:"listen_addr"`
}

// DBConfig defines the database configuration.
type DBConfig struct {
	Path            string `mapstructure:"path"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // seconds
}

// SchedulerConfig defines the scheduler configuration.
type SchedulerConfig struct {
	RotationInterval time.Duration `mapstructure:"rotation_interval"`
	CleanupInterval  time.Duration `mapstructure:"cleanup_interval"`
	CleanupAge       time.Duration `mapstructure:"cleanup_age"`
}

// ServiceConfig defines service-level configuration options.
type ServiceConfig struct {
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// HetznerConfig defines the Hetzner provider configuration.
type HetznerConfig struct {
	APIToken          string   `mapstructure:"api_token"`
	ServerTypes       []string `mapstructure:"server_types"`
	Image             string   `mapstructure:"image"`
	Locations         []string `mapstructure:"locations"`
	SSHKey            string   `mapstructure:"ssh_key"`
	SSHPrivateKeyPath string   `mapstructure:"ssh_private_key_path"`
}
