package poller

import (
	"context"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// EventHandler handles events during polling.
type EventHandler interface {
	OnNewNodeDetected(oldConfig, newConfig *client.NodeConfig) error
	OnPollError(err error)
	OnConnectionRestored()
}

// Poller manages polling for node rotation.
type Poller struct {
	client        *client.Client
	interval      time.Duration
	eventHandler  EventHandler
	currentConfig *client.NodeConfig
	logger        *logger.Logger
}

// New creates a new poller.
func New(apiURL string, pollIntervalMinutes int, handler EventHandler, log *logger.Logger) *Poller {
	if log == nil {
		log = logger.New("info", "text")
	}

	return &Poller{
		client:       client.NewClient(apiURL, log),
		interval:     time.Duration(pollIntervalMinutes) * time.Minute,
		eventHandler: handler,
		logger:       log,
	}
}

// Start begins polling for node rotation.
func (p *Poller) Start(ctx context.Context, initialConfig *client.NodeConfig) error {
	p.currentConfig = initialConfig
	p.logger.Info("starting node rotation poller", "interval_minutes", int(p.interval.Minutes()))

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Poll every interval to check for node rotation
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("poller stopped due to context cancellation")
			return ctx.Err()
		case <-ticker.C:
			if err := p.pollOnce(ctx); err != nil {
				p.logger.Error("error during polling", "error", err)
				p.eventHandler.OnPollError(err)
			}
		}
	}
}

// pollOnce performs a single poll operation.
func (p *Poller) pollOnce(ctx context.Context) error {
	newConfig, err := p.client.FetchNodeConfig(ctx)
	if err != nil {
		return err
	}

	// Detect new node by comparing server details
	if p.isNewNode(p.currentConfig, newConfig) {
		p.logger.Info("new node detected, triggering rotation",
			"old_ip", p.currentConfig.ServerIP,
			"new_ip", newConfig.ServerIP)

		// If new node detected, trigger reconnection
		if err := p.eventHandler.OnNewNodeDetected(p.currentConfig, newConfig); err != nil {
			p.logger.Error("failed to handle new node", "error", err)
			return err
		}

		// Update current config to the new one
		p.currentConfig = newConfig
		p.logger.Info("successfully rotated to new node")
	} else {
		p.logger.Debug("no node rotation needed", "current_ip", newConfig.ServerIP)
	}

	return nil
}

// isNewNode determines if the new configuration represents a different node.
func (p *Poller) isNewNode(oldConfig, newConfig *client.NodeConfig) bool {
	if oldConfig == nil {
		return true // First poll, consider it a new node
	}

	return oldConfig.ServerPublicKey != newConfig.ServerPublicKey ||
		oldConfig.ServerIP != newConfig.ServerIP
}
