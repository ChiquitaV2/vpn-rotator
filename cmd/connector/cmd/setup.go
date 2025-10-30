package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
)

// setupCmd helps users set up their VPN Rotator configuration
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up VPN Rotator configuration",
	Long: `Set up VPN Rotator with intelligent defaults and create configuration files.
This command creates a default configuration file in your home directory
and sets up the necessary directory structure.

Examples:
  # Create default configuration
  vpn-rotator setup

  # Create configuration with custom API URL
  vpn-rotator setup --api-url http://my-server:8080`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Setting up VPN Rotator...\n\n")

		// Create default configuration
		if err := config.CreateDefaultConfig(); err != nil {
			if err.Error() == "config file already exists" {
				fmt.Printf("Configuration already exists\n")
				fmt.Printf("Edit ~/.vpn-rotator/.vpn-rotator.yaml to customize settings\n\n")
			} else {
				fmt.Printf("Failed to create configuration: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("Created default configuration\n\n")
		}

		// Load the configuration to show current settings
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Failed to load configuration: %v\n", err)
			os.Exit(1)
		}

		// Override with command line flags if provided
		if apiURL, _ := cmd.Flags().GetString("api-url"); apiURL != "" {
			cfg.APIURL = apiURL
			fmt.Printf("Using custom API URL: %s\n", apiURL)
		}

		// Display current configuration
		fmt.Printf("Current Configuration:\n")
		fmt.Printf("=====================\n")
		fmt.Printf("API URL: %s\n", cfg.APIURL)
		fmt.Printf("Interface: %s\n", cfg.Interface)
		fmt.Printf("Key Path: %s\n", cfg.KeyPath)
		fmt.Printf("Generate Keys: %t\n", cfg.GenerateKeys)
		fmt.Printf("Poll Interval: %d minutes\n", cfg.PollInterval)
		fmt.Printf("Log Level: %s\n", cfg.LogLevel)

		fmt.Printf("\n✨ Setup complete!\n\n")

		// Provide next steps
		fmt.Printf("Next Steps:\n")
		fmt.Printf("===========\n")
		fmt.Printf("1. Make sure your VPN Rotator server is running at: %s\n", cfg.APIURL)
		fmt.Printf("2. Connect to VPN: vpn-rotator simple-connect\n")
		fmt.Printf("3. Check status: vpn-rotator status\n")
		fmt.Printf("4. Disconnect: vpn-rotator simple-disconnect\n\n")

		fmt.Printf("Tips:\n")
		fmt.Printf("   - Use --generate-keys to let the server generate keys for you\n")
		fmt.Printf("   - Edit ~/.vpn-rotator/.vpn-rotator.yaml to customize settings\n")
		fmt.Printf("   - Use VPN_ROTATOR_* environment variables to override settings\n")
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)

	// Add setup-specific flags
	setupCmd.Flags().String("api-url", "", "VPN Rotator API URL to use in configuration")
}
