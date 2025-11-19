package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector"
	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
)

// statusCmd shows the current VPN connection status
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show VPN connection status",
	Long: `Show the current status of the VPN connection including:
- Connection state (connected/disconnected)
- Peer ID (if connected)
- Interface name
- Key file location
- Configuration details

Examples:
  # Show current status
  vpn-rotator status`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration with intelligent defaults
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Set up logger (quiet for status command)
		loggerConfig := logger.LoggerConfig{
			Level:     logger.LevelError,
			Format:    logger.OutputFormat(cfg.LogFormat),
			Component: "connector",
			Version:   "1.0.0",
		}
		log := logger.New(loggerConfig)

		// Create simplified connector
		conn := connector.NewConnector(cfg, log)

		// Try to restore previous connection state
		if err := conn.LoadConnectionState(); err != nil {
			log.Warn("failed to load connection state", "error", err)
		}

		// Display status
		fmt.Printf("VPN Rotator Connection Status\n")
		fmt.Printf("============================\n\n")

		// Connection status
		if conn.IsConnected() {
			fmt.Printf("Status: CONNECTED\n")
			if peerID := conn.GetPeerID(); peerID != "" {
				fmt.Printf("Peer ID: %s\n", peerID)
			}
		} else {
			fmt.Printf("Status: DISCONNECTED\n")
		}

		// Configuration details
		fmt.Printf("\nConfiguration:\n")
		fmt.Printf("  API URL: %s\n", cfg.APIURL)
		fmt.Printf("  Interface: %s\n", cfg.Interface)
		fmt.Printf("  Key Path: %s\n", cfg.KeyPath)
		fmt.Printf("  Poll Interval: %d minutes\n", cfg.PollInterval)
		fmt.Printf("  Generate Keys: %t\n", cfg.GenerateKeys)

		// Key file status
		fmt.Printf("\nKey File:\n")
		if _, err := os.Stat(cfg.KeyPath); err == nil {
			fmt.Printf("  Status: EXISTS\n")
			fmt.Printf("  Path: %s\n", cfg.KeyPath)
		} else {
			fmt.Printf("  Status: NOT FOUND\n")
			fmt.Printf("  Expected Path: %s\n", cfg.KeyPath)
		}

		// Network interface status
		fmt.Printf("\nNetwork Interface:\n")
		if conn.IsConnected() {
			fmt.Printf("  Status: ACTIVE\n")
			fmt.Printf("  Name: %s\n", cfg.Interface)
		} else {
			fmt.Printf("  Status: INACTIVE\n")
			fmt.Printf("  Name: %s (not active)\n", cfg.Interface)
		}

		// Check provisioning status
		fmt.Printf("\nProvisioning Status:\n")
		apiClient := client.NewClient(cfg.APIURL, log)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if provisioningInfo, err := apiClient.GetProvisioningStatus(ctx); err != nil {
			fmt.Printf("  Status: UNAVAILABLE (%v)\n", err)
		} else {
			if provisioningInfo.IsActive {
				fmt.Printf("  Status: IN PROGRESS\n")
				fmt.Printf("  Phase: %s\n", provisioningInfo.Phase)
				fmt.Printf("  Progress: %.1f%%\n", provisioningInfo.Progress*100)
				if provisioningInfo.EstimatedETA != nil {
					remaining := time.Until(*provisioningInfo.EstimatedETA)
					if remaining > 0 {
						fmt.Printf("  ETA: %v\n", remaining.Round(time.Second))
					}
				}
			} else {
				fmt.Printf("  Status: IDLE\n")
			}
		}

		// Check overall health
		if healthResp, err := apiClient.GetHealth(ctx); err != nil {
			fmt.Printf("\nService Health: UNAVAILABLE (%v)\n", err)
		} else {
			fmt.Printf("\nService Health: %s\n", healthResp.Status)
			if healthResp.Provisioning != nil && healthResp.Provisioning.IsActive {
				fmt.Printf("  Provisioning active in health check\n")
			}
		}

		fmt.Printf("\n")

		// Provide helpful next steps
		if !conn.IsConnected() {
			fmt.Printf("To connect: vpn-rotator connect\n")
			fmt.Printf("To connect without waiting: vpn-rotator connect --no-wait\n")
		} else {
			fmt.Printf("To disconnect: vpn-rotator disconnect\n")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
