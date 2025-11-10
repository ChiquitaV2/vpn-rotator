package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// disconnectCmd provides a streamlined disconnection experience
var disconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Disconnect from VPN with automatic cleanup",
	Long: `Disconnect from VPN with intelligent cleanup of connections and state.
This command automatically discovers the current connection and cleanly
disconnects from the VPN server.

Examples:
  # Disconnect with automatic discovery
  vpn-rotator disconnect

  # Disconnect and remove local keys
  vpn-rotator disconnect --cleanup-keys`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration with intelligent defaults
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Override with command line flags
		if iface, _ := cmd.Flags().GetString("interface"); iface != "" {
			cfg.Interface = iface
		}

		// Set up logger
		loggerConfig := logger.LoggerConfig{
			Level:     logger.LogLevel(cfg.LogLevel),
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

		// Check if connected
		if !conn.IsConnected() {
			log.Info("VPN is not connected", "interface", cfg.Interface)
			fmt.Printf("VPN is not connected.\n")
			return
		}

		log.Info("starting VPN disconnection")

		// Disconnect from VPN
		if err := conn.Disconnect(); err != nil {
			log.Error("disconnection failed", "error", err)
			fmt.Printf("Disconnection failed: %v\n", err)
			os.Exit(1)
		}

		// Handle key cleanup if requested
		if cleanupKeys, _ := cmd.Flags().GetBool("cleanup-keys"); cleanupKeys {
			if err := cleanupSimpleLocalKeys(cfg, log); err != nil {
				log.Warn("failed to cleanup keys", "error", err)
				fmt.Printf("Warning: Failed to cleanup keys: %v\n", err)
			} else {
				fmt.Printf("Cleaned up local keys\n")
			}
		}

		fmt.Printf("Disconnected successfully!\n")
		log.Info("disconnection completed successfully")
	},
}

// cleanupSimpleLocalKeys removes local private key files
func cleanupSimpleLocalKeys(cfg *config.Config, log *logger.Logger) error {
	if cfg.KeyPath == "" {
		return nil
	}

	if err := os.Remove(cfg.KeyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove key file: %w", err)
	}

	log.Info("removed local private key", "key_path", cfg.KeyPath)
	return nil
}

func init() {
	rootCmd.AddCommand(disconnectCmd)

	// Add essential flags only
	disconnectCmd.Flags().String("interface", "", "WireGuard interface name (default: wg0)")
	disconnectCmd.Flags().Bool("cleanup-keys", false, "Remove local private key files after disconnect")
}
