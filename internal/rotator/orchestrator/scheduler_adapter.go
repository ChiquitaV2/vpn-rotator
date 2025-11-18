package orchestrator

import (
	"context"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
)

// SchedulerVPNService combines node rotation and cleanup operations for the scheduler.
// This adapter provides the interface expected by the scheduler while delegating
// to the appropriate services.
type SchedulerVPNService struct {
	nodeOrchestrator       NodeOrchestrator
	resourceCleanupService *services.ResourceCleanupService
}

// NewSchedulerVPNService creates an adapter for scheduler operations
func NewSchedulerVPNService(
	nodeOrchestrator NodeOrchestrator,
	resourceCleanupService *services.ResourceCleanupService,
) *SchedulerVPNService {
	return &SchedulerVPNService{
		nodeOrchestrator:       nodeOrchestrator,
		resourceCleanupService: resourceCleanupService,
	}
}

// RotateNodes delegates to NodeOrchestrator
func (s *SchedulerVPNService) RotateNodes(ctx context.Context) error {
	return s.nodeOrchestrator.RotateNodes(ctx)
}

// CleanupInactiveResources delegates to ResourceCleanupService directly
func (s *SchedulerVPNService) CleanupInactiveResources(ctx context.Context) error {
	return s.resourceCleanupService.CleanupInactiveResources(ctx)
}
