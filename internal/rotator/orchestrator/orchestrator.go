package orchestrator

import (
	"log/slog"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ipmanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/nodemanager"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peermanager"
)

// Orchestrator orchestrates VPN node lifecycle: provisioning, rotation, and cleanup.
type Orchestrator struct {
	store       db.Store
	nodeManager nodemanager.NodeManager
	peerManager peermanager.Manager
	ipManager   ipmanager.IPManager
	logger      *slog.Logger
}

// New creates a new Orchestrator.
func New(store db.Store, nodeManager nodemanager.NodeManager, peerManager peermanager.Manager, ipManager ipmanager.IPManager, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{
		store:       store,
		nodeManager: nodeManager,
		peerManager: peerManager,
		ipManager:   ipManager,
		logger:      logger,
	}
}
