package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
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
		log := logger.New("error", cfg.LogFormat)

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

		fmt.Printf("\n")

		// Provide helpful next steps
		if !conn.IsConnected() {
			fmt.Printf("To connect: vpn-rotator simple-connect\n")
		} else {
			fmt.Printf("To disconnect: vpn-rotator simple-disconnect\n")
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
