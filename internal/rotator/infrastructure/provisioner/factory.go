package provisioner

import (
	"fmt"
	"log/slog"
)

// ProviderType represents the type of cloud provider
type ProviderType string

const (
	ProviderTypeHetzner ProviderType = "hetzner"
	// Add more providers as needed
	// ProviderTypeAWS     ProviderType = "aws"
	// ProviderTypeGCP     ProviderType = "gcp"
)

// Config represents the configuration for creating a cloud provisioner
type Config struct {
	Provider ProviderType `json:"provider"`

	// Hetzner-specific config
	Hetzner *HetznerConfig `json:"hetzner,omitempty"`

	// Add other provider configs as needed
	// AWS     *AWSConfig     `json:"aws,omitempty"`
	// GCP     *GCPConfig     `json:"gcp,omitempty"`
}

// NewCloudProvisioner creates a new cloud provisioner based on the configuration
func NewCloudProvisioner(config *Config, apiToken string, logger *slog.Logger) (CloudProvisioner, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	switch config.Provider {
	case ProviderTypeHetzner:
		if config.Hetzner == nil {
			return nil, fmt.Errorf("Hetzner config is required for Hetzner provider")
		}
		return NewHetznerProvisioner(apiToken, config.Hetzner, logger)

	// Add more providers as needed
	// case ProviderTypeAWS:
	//     if config.AWS == nil {
	//         return nil, fmt.Errorf("AWS config is required for AWS provider")
	//     }
	//     return NewAWSProvisioner(apiToken, config.AWS, logger)

	default:
		return nil, fmt.Errorf("unsupported provider type: %s", config.Provider)
	}
}

// DefaultHetznerConfig returns a default Hetzner configuration
func DefaultHetznerConfig() *HetznerConfig {
	return &HetznerConfig{
		ServerType:   "cx11",
		Image:        "ubuntu-22.04",
		Location:     "nbg1",
		SSHPublicKey: "",
	}
}
