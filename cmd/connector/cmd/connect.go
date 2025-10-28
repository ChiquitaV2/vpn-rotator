package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/connector/poller"
	"github.com/chiquitav2/vpn-rotator/internal/connector/wireguard"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to VPN",
	Long: `Connect to VPN using configuration from the Rotator Service.
The command will fetch the latest configuration, generate WireGuard config,
establish the VPN connection, and automatically poll for rotations.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration using hybrid config loader
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Set up enhanced logger
		log := logger.New(cfg.LogLevel, cfg.LogFormat)

		log.Info("starting VPN connection")

		// Perform the connection with polling
		if err := performConnection(cfg, log); err != nil {
			log.Error("connection error", "error", err)
			os.Exit(1)
		}
	},
}

// connectionHandler implements the poller.EventHandler interface.
type connectionHandler struct {
	configGenerator *wireguard.ConfigGenerator
	cfg             *config.Config
	logger          *logger.Logger
}

// OnNewNodeDetected handles when a new node is detected.
func (ch *connectionHandler) OnNewNodeDetected(oldConfig, newConfig *client.NodeConfig) error {
	ch.logger.Info("new node detected, migrating connection",
		"old_ip", oldConfig.ServerIP,
		"new_ip", newConfig.ServerIP,
		"old_port", oldConfig.Port,
		"new_port", newConfig.Port,
	)

	// Disconnect from current node
	if err := ch.configGenerator.RemoveConfig(ch.cfg.Interface); err != nil {
		ch.logger.Warn("failed to disconnect from old node", "error", err)
		return err
	}

	// Generate new configuration
	configContent, err := ch.configGenerator.GenerateConfig(
		ch.cfg.KeyPath,
		newConfig.ServerPublicKey,
		newConfig.ServerIP,
		newConfig.Port,
		ch.cfg.AllowedIPs,
		ch.cfg.DNS,
	)
	if err != nil {
		err := fmt.Errorf("failed to generate new WireGuard config: %w", err)
		ch.logger.Error("failed to generate new config", "error", err)
		return err
	}

	// Validate the configuration
	if err := ch.configGenerator.ValidateConfig(configContent); err != nil {
		err := fmt.Errorf("invalid configuration: %w", err)
		ch.logger.Error("configuration validation failed", "error", err)
		return err
	}

	// Write to temporary file
	tempConfigPath := fmt.Sprintf("/tmp/wg-%s.conf", ch.cfg.Interface)
	if err := ch.configGenerator.WriteConfigFile(configContent, tempConfigPath); err != nil {
		err := fmt.Errorf("failed to write config file: %w", err)
		ch.logger.Error("failed to write config file", "error", err)
		return err
	}

	// Connect to new node
	if err := ch.configGenerator.ApplyConfig(tempConfigPath, ch.cfg.Interface); err != nil {
		err := fmt.Errorf("failed to connect to new node: %w", err)
		ch.logger.Error("failed to connect to new node", "error", err)
		return err
	}

	ch.logger.Info("successfully migrated to new node", "new_ip", newConfig.ServerIP)

	return nil
}

// OnPollError handles errors during polling.
func (ch *connectionHandler) OnPollError(err error) {
	ch.logger.Error("polling error", "error", err)
}

// OnConnectionRestored handles when connection is restored after error.
func (ch *connectionHandler) OnConnectionRestored() {
	ch.logger.Info("connection restored after error")
}

// performConnection handles the full connection process including polling.
func performConnection(cfg *config.Config, log *logger.Logger) error {
	// Ensure we have the required system tools
	if !hasWireGuard() {
		return fmt.Errorf("WireGuard tools not found. Please install wireguard-tools or wg-quick")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create channel to handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to handle signals
	go func() {
		sig := <-sigChan
		log.Info("received shutdown signal, disconnecting", "signal", sig)

		// Create config generator to clean up connection
		configGenerator := wireguard.NewConfigGenerator(log)
		configGenerator.RemoveConfig(cfg.Interface)

		cancel()
	}()

	// Create API client
	apiClient := client.NewClient(cfg.APIURL, log)

	// Fetch initial configuration
	vpnConfig, err := apiClient.FetchNodeConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch initial VPN config: %w", err)
	}

	// Create config generator
	configGenerator := wireguard.NewConfigGenerator(log)

	// Generate WireGuard configuration
	configContent, err := configGenerator.GenerateConfig(
		cfg.KeyPath,
		vpnConfig.ServerPublicKey,
		vpnConfig.ServerIP,
		vpnConfig.Port,
		cfg.AllowedIPs,
		cfg.DNS,
	)
	if err != nil {
		return fmt.Errorf("failed to generate WireGuard config: %w", err)
	}

	// Validate the configuration before applying
	if err := configGenerator.ValidateConfig(configContent); err != nil {
		return fmt.Errorf("invalid WireGuard config: %w", err)
	}

	// Write to temporary file
	tempConfigPath := fmt.Sprintf("/tmp/wg-%s.conf", cfg.Interface)
	if err := configGenerator.WriteConfigFile(configContent, tempConfigPath); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Apply the configuration
	if err := configGenerator.ApplyConfig(tempConfigPath, cfg.Interface); err != nil {
		return fmt.Errorf("failed to apply WireGuard config: %w", err)
	}

	log.Info("connected to VPN node", "server_ip", vpnConfig.ServerIP, "server_port", vpnConfig.Port)

	// Create connection handler
	handler := &connectionHandler{
		configGenerator: configGenerator,
		cfg:             cfg,
		logger:          log,
	}

	// Create and start poller
	p := poller.New(cfg.APIURL, cfg.PollInterval, handler, log)

	// Start polling in a separate goroutine
	go func() {
		if err := p.Start(ctx, vpnConfig); err != nil && err != context.Canceled {
			log.Error("polling error", "error", err)
		}
	}()

	// Wait for context to be cancelled (signal received)
	<-ctx.Done()

	log.Info("connection process completed")
	return nil
}

// hasWireGuard checks if WireGuard tools are available.
func hasWireGuard() bool {
	_, err := exec.LookPath("wg-quick")
	return err == nil
}

func init() {
	rootCmd.AddCommand(connectCmd)

	// Add flags for the connect command
	connectCmd.Flags().String("api-url", "", "Rotator Service API URL")
	connectCmd.Flags().String("key-path", "", "Path to client private key")
	connectCmd.Flags().String("interface", "", "WireGuard interface name")
	connectCmd.Flags().Int("poll-interval", 0, "Minutes between rotation checks")
	connectCmd.Flags().String("allowed-ips", "", "VPN allowed IPs")
	connectCmd.Flags().String("dns", "", "DNS servers")
	// Bind flags to viper so they can be accessed via viper.Get
	viper.BindPFlag("api_url", connectCmd.Flags().Lookup("api-url"))
	viper.BindPFlag("key_path", connectCmd.Flags().Lookup("key-path"))
	viper.BindPFlag("interface", connectCmd.Flags().Lookup("interface"))
	viper.BindPFlag("poll_interval", connectCmd.Flags().Lookup("poll-interval"))
	viper.BindPFlag("allowed_ips", connectCmd.Flags().Lookup("allowed-ips"))
	viper.BindPFlag("dns", connectCmd.Flags().Lookup("dns"))

}
