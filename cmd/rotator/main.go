package main

import (
	"context"
	"os"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/shared/logger"
	_ "github.com/mattn/go-sqlite3"
)

const version = "1.0.0"

func main() {
	// Initialize logger first
	log := logger.New("info", "json")
	log.Info("starting vpn-rotator", "version", version)

	// Load configuration
	loader := config.NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		log.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Update logger with configured log level and format
	log = logger.New(cfg.Log.Level, cfg.Log.Format)
	log.Info("configuration loaded successfully")

	// Create service instance
	service, err := rotator.NewService(cfg, log.Logger)
	if err != nil {
		log.Error("failed to create service", "error", err)
		os.Exit(1)
	}

	// Start service with proper error handling
	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		log.Error("failed to start service", "error", err)

		// Attempt graceful cleanup if service creation succeeded but startup failed
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if stopErr := service.Stop(shutdownCtx); stopErr != nil {
			log.Error("failed to cleanup service after startup failure", "error", stopErr)
		}

		os.Exit(1)
	}

	log.Info("service started successfully, waiting for shutdown signal")

	// Wait for shutdown signal - this blocks until SIGINT/SIGTERM is received
	// The service handles signal registration and graceful shutdown internally
	service.WaitForShutdown()

	log.Info("main process exiting")
}
