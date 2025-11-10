package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/chiquitav2/vpn-rotator/internal/connector/client"
	"github.com/chiquitav2/vpn-rotator/internal/connector/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
)

// provisioningCmd shows and monitors provisioning status
var provisioningCmd = &cobra.Command{
	Use:   "provisioning",
	Short: "Monitor node provisioning status",
	Long: `Monitor the current node provisioning status and progress.
This command shows real-time information about ongoing provisioning operations
including phase, progress percentage, and estimated completion time.

Examples:
  # Show current provisioning status
  vpn-rotator provisioning

  # Monitor provisioning with live updates
  vpn-rotator provisioning --watch

  # Monitor with custom update interval
  vpn-rotator provisioning --watch --interval 5s`,
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration
		loader := config.NewLoader()
		cfg, err := loader.Load()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		// Override API URL if provided
		if apiURL, _ := cmd.Flags().GetString("api-url"); apiURL != "" {
			cfg.APIURL = apiURL
		}

		// Set up logger
		loggerConfig := logger.LoggerConfig{
			Level:     logger.LevelError,
			Format:    logger.OutputFormat(cfg.LogFormat),
			Component: "connector",
			Version:   "1.0.0",
		}
		log := logger.New(loggerConfig)

		// Create API client
		apiClient := client.NewClient(cfg.APIURL, log)

		// Get flags
		watch, _ := cmd.Flags().GetBool("watch")
		interval, _ := cmd.Flags().GetDuration("interval")

		if watch {
			monitorProvisioning(apiClient, interval)
		} else {
			showProvisioningStatus(apiClient)
		}
	},
}

func showProvisioningStatus(apiClient *client.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Printf("Node Provisioning Status\n")
	fmt.Printf("=======================\n\n")

	provisioningInfo, err := apiClient.GetProvisioningStatus(ctx)
	if err != nil {
		fmt.Printf("❌ Failed to get provisioning status: %v\n", err)
		return
	}

	if !provisioningInfo.IsActive {
		fmt.Printf("✅ No active provisioning\n")
		fmt.Printf("   System is ready for new connections\n")
		return
	}

	fmt.Printf("🔄 Provisioning in progress\n")
	fmt.Printf("   Phase: %s\n", provisioningInfo.Phase)
	fmt.Printf("   Progress: %.1f%%\n", provisioningInfo.Progress*100)

	if provisioningInfo.EstimatedETA != nil {
		remaining := time.Until(*provisioningInfo.EstimatedETA)
		if remaining > 0 {
			fmt.Printf("   Estimated completion: %v\n", remaining.Round(time.Second))
		} else {
			fmt.Printf("   Estimated completion: any moment now\n")
		}
	}

	fmt.Printf("\n💡 Tip: Use --watch to monitor progress in real-time\n")
}

func monitorProvisioning(apiClient *client.Client, interval time.Duration) {
	fmt.Printf("Monitoring provisioning status (press Ctrl+C to stop)\n")
	fmt.Printf("Update interval: %v\n\n", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Show initial status
	showProvisioningStatusLine(apiClient)

	for {
		select {
		case <-ticker.C:
			showProvisioningStatusLine(apiClient)
		}
	}
}

func showProvisioningStatusLine(apiClient *client.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	timestamp := time.Now().Format("15:04:05")

	provisioningInfo, err := apiClient.GetProvisioningStatus(ctx)
	if err != nil {
		fmt.Printf("[%s] ❌ Error: %v\n", timestamp, err)
		return
	}

	if !provisioningInfo.IsActive {
		fmt.Printf("[%s] ✅ Idle - Ready for connections\n", timestamp)
		return
	}

	progressBar := generateProgressBar(provisioningInfo.Progress, 20)
	eta := ""
	if provisioningInfo.EstimatedETA != nil {
		remaining := time.Until(*provisioningInfo.EstimatedETA)
		if remaining > 0 {
			eta = fmt.Sprintf(" (ETA: %v)", remaining.Round(time.Second))
		}
	}

	fmt.Printf("[%s] 🔄 %s %s %.1f%%%s\n",
		timestamp,
		provisioningInfo.Phase,
		progressBar,
		provisioningInfo.Progress*100,
		eta)
}

func generateProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}

	bar := "["
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	bar += "]"
	return bar
}

func init() {
	rootCmd.AddCommand(provisioningCmd)

	provisioningCmd.Flags().String("api-url", "", "VPN Rotator API URL")
	provisioningCmd.Flags().Bool("watch", false, "Monitor provisioning status with live updates")
	provisioningCmd.Flags().Duration("interval", 5*time.Second, "Update interval for watch mode")
}
