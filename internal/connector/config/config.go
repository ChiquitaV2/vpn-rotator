package config

// Config holds the connector application configuration.
type Config struct {
	APIURL       string `mapstructure:"api_url"`
	Interface    string `mapstructure:"interface"`
	PollInterval int    `mapstructure:"poll_interval"`
	AllowedIPs   string `mapstructure:"allowed_ips"`
	DNS          string `mapstructure:"dns"`
	LogLevel     string `mapstructure:"log_level"`
	LogFormat    string `mapstructure:"log_format"`

	// Simplified key management - automatically discovers keys
	KeyPath      string `mapstructure:"key_path"`
	GenerateKeys bool   `mapstructure:"generate_keys"`
}
