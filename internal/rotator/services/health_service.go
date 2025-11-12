package services

import (
	"context"
)

// HealthService defines the application layer interface for health checks.
type HealthService interface {
	GetHealth(ctx context.Context) (*HealthResponse, error)
}

// healthService implements HealthService.
type healthService struct {
	provisioningService *ProvisioningService
}

// NewHealthService creates a new health service.
func NewHealthService(provisioningService *ProvisioningService) HealthService {
	return &healthService{
		provisioningService: provisioningService,
	}
}

// GetHealth returns the service health status.
func (s *healthService) GetHealth(ctx context.Context) (*HealthResponse, error) {
	response := &HealthResponse{
		Status:  "healthy",
		Version: "1.0.0",
	}

	if s.provisioningService != nil {
		provisioningStatus := s.provisioningService.GetCurrentStatus()
		if provisioningStatus != nil {
			response.Provisioning = &ProvisioningInfo{
				IsActive:     provisioningStatus.IsActive,
				Phase:        provisioningStatus.Phase,
				Progress:     provisioningStatus.Progress,
				EstimatedETA: provisioningStatus.EstimatedETA,
			}
		}
	}

	return response, nil
}
