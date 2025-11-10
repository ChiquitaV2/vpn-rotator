package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector"
	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// connectCmd provides a streamlined connection experience
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to VPN with automatic configuration",
	Long: `Connect to VPN with intelligent defaults and automatic key management.
This command automatically discovers configuration files and keys, making it
easy to get started with minimal setup.

Examples:
  # Connect with automatic discovery
  vpn-rotator connect

  # Connect with server-generated keys
  vpn-rotator connect --generate-keys

  # Connect with custom API URL
  vpn-rotator connect --api-url http://my-server:8080`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration with intelligent defaults
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Override with command line flags
		if apiURL, _ := cmd.Flags().GetString("api-url"); apiURL != "" {
			cfg.APIURL = apiURL
		}
		if generateKeys, _ := cmd.Flags().GetBool("generate-keys"); generateKeys {
			cfg.GenerateKeys = true
		}
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

		// Check if already connected
		if conn.IsConnected() {
			log.Info("VPN is already connected", "peer_id", conn.GetPeerID(), "interface", cfg.Interface)
			fmt.Printf("Already connected! Peer ID: %s\n", conn.GetPeerID())
			fmt.Printf("Use 'vpn-rotator disconnect' to disconnect.\n")
			return
		}

		log.Info("starting simplified VPN connection")

		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Set up signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Handle shutdown signals
		go func() {
			sig := <-sigChan
			log.Info("received shutdown signal, disconnecting", "signal", sig)

			if err := conn.Disconnect(); err != nil {
				log.Error("failed to disconnect cleanly", "error", err)
			}

			cancel()
		}()

		// Connect to VPN with provisioning wait handling
		if err := conn.Reconnect(ctx); err != nil {
			log.Error("reconnection failed", "error", err)
			fmt.Printf("Reconnection failed: %v\n", err)
		}

		// Configure provisioning wait behavior based on flags
		maxWaits, _ := cmd.Flags().GetInt("max-provisioning-waits")
		statusInterval, _ := cmd.Flags().GetDuration("provisioning-check-interval")
		noWait, _ := cmd.Flags().GetBool("no-wait")

		conn.SetProvisioningWaitConfig(maxWaits, statusInterval)

		if noWait {
			// Connect without waiting for provisioning
			if err = conn.ConnectWithoutWait(ctx); err != nil {
				// Check if this is a provisioning error
				if provErr, ok := err.(*client.ProvisioningInProgressError); ok {
					fmt.Printf("\n🔄 Node provisioning is required\n")
					fmt.Printf("   Message: %s\n", provErr.Message)
					fmt.Printf("   Estimated wait: %d seconds\n", provErr.EstimatedWait)
					fmt.Printf("   Retry after: %d seconds\n", provErr.RetryAfter)
					fmt.Printf("\nTo wait automatically for provisioning, run without --no-wait flag\n")
					os.Exit(2) // Different exit code for provisioning needed
				}
				log.Error("connection failed", "error", err)
				fmt.Printf("Connection failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Connect with automatic provisioning wait
			if err = conn.ConnectWithProvisioningFeedback(ctx); err != nil {
				log.Error("connection failed", "error", err)
				fmt.Printf("Connection failed: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Connected successfully!\n")
		fmt.Printf("   Peer ID: %s\n", conn.GetPeerID())
		fmt.Printf("   Interface: %s\n", cfg.Interface)
		fmt.Printf("   Monitoring for rotations every %d minutes\n", cfg.PollInterval)
		fmt.Printf("\nPress Ctrl+C to disconnect\n")

		// Start rotation monitoring
		go func() {
			if err := conn.MonitorRotation(ctx); err != nil && err != context.Canceled {
				log.Error("rotation monitoring error", "error", err)
			}
		}()

		// Wait for shutdown signal
		<-ctx.Done()

		log.Info("connection process completed")
		fmt.Printf("Disconnected.\n")
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)

	// Add essential flags only
	connectCmd.Flags().String("api-url", "", "VPN Rotator API URL")
	connectCmd.Flags().String("interface", "", "WireGuard interface name (default: wg0)")
	connectCmd.Flags().Bool("generate-keys", false, "Use server-generated keys instead of local keys")
	connectCmd.Flags().String("key-path", "", "Custom path for private key file")

	// Provisioning wait configuration
	connectCmd.Flags().Int("max-provisioning-waits", 5, "Maximum number of provisioning wait cycles")
	connectCmd.Flags().Duration("provisioning-check-interval", 15*time.Second, "Interval for checking provisioning status")
	connectCmd.Flags().Bool("no-wait", false, "Don't wait for provisioning, return immediately if provisioning is needed")
}
