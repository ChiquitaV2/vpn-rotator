package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/connector/wireguard"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// disconnectCmd represents the disconnect command
var disconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Disconnect from VPN",
	Long: `Disconnect from VPN by stopping the WireGuard interface.
This command will stop the active VPN connection.`,
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

		log.Info("starting VPN disconnection")

		// Disconnect from VPN
		configGenerator := wireguard.NewConfigGenerator(log)
		if err := configGenerator.RemoveConfig(cfg.Interface); err != nil {
			log.Error("failed to disconnect", "error", err)
			os.Exit(1)
		}

		log.Info("successfully disconnected from VPN", "interface", cfg.Interface)
	},
}

func init() {
	rootCmd.AddCommand(disconnectCmd)

	// Add flags for the disconnect command
	disconnectCmd.Flags().StringP("interface", "i", "", "WireGuard interface name")

	// Bind flags to viper so they can be accessed via viper.Get
	viper.BindPFlag("interface", disconnectCmd.Flags().Lookup("interface"))
}
