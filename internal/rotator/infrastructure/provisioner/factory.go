package provisioner

import (
	"fmt"

	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
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
func NewCloudProvisioner(config *Config, apiToken string, logger *applogger.Logger) (CloudProvisioner, error) { // <-- Changed
	if config == nil {
		return nil, apperrors.NewSystemError(apperrors.ErrCodeConfiguration, "provisioner config is required", false, nil)
	}

	scopedLogger := logger.WithComponent("provisioner.factory")

	switch config.Provider {
	case ProviderTypeHetzner:
		if config.Hetzner == nil {
			return nil, apperrors.NewSystemError(apperrors.ErrCodeConfiguration, "Hetzner config is required for Hetzner provider", false, nil)
		}
		// Pass the scoped logger to the Hetzner provisioner
		return NewHetznerProvisioner(apiToken, config.Hetzner, scopedLogger)

	// ... (other providers) ...

	default:
		return nil, apperrors.NewSystemError(apperrors.ErrCodeConfiguration, fmt.Sprintf("unsupported provider type: %s", config.Provider), false, nil)
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
