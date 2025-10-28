package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show VPN connection status",
	Long: `Show the current status of the VPN connection including:
- Connection status
- Current node information
- Interface statistics`,
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

		log.Info("checking VPN connection status")

		// Check status by running wg command to get interface stats
		cmdExec := exec.Command("wg", "show", cfg.Interface)
		output, err := cmdExec.Output()
		if err != nil {
			fmt.Printf("Interface %s is not active or WireGuard is not running\n", cfg.Interface)
			return
		}

		fmt.Printf("Interface: %s\n", cfg.Interface)
		fmt.Printf("WireGuard status:\n%s\n", string(output))
		fmt.Println("Status: Connected")
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)

	// Add flags for the status command
	statusCmd.Flags().StringP("interface", "i", "", "WireGuard interface name")

	// Bind flags to viper so they can be accessed via viper.Get
	viper.BindPFlag("interface", statusCmd.Flags().Lookup("interface"))
}
