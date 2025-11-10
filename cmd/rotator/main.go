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
	ctx := context.Background()

	// Initialize logger first
	log := logger.NewProduction("rotator", version)
	log.InfoContext(ctx, "starting vpn-rotator", "version", version)

	// Load configuration
	loader := config.NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		log.ErrorCtx(ctx, "failed to load configuration", err)
		os.Exit(1)
	}

	// Update logger with configured settings
	loggerConfig := logger.LoggerConfig{
		Level:     logger.LogLevel(cfg.Log.Level),
		Format:    logger.OutputFormat(cfg.Log.Format),
		Component: "rotator",
		Version:   version,
	}
	log = logger.New(loggerConfig)
	log.DebugContext(ctx, "configuration loaded successfully")

	// Create service instance
	service, err := rotator.NewService(cfg, log)
	if err != nil {
		log.ErrorCtx(ctx, "failed to create service", err)
		os.Exit(1)
	}

	// Start service with proper error handling
	if err := service.Start(ctx); err != nil {
		log.ErrorCtx(ctx, "failed to start service", err)

		// Attempt graceful cleanup if service creation succeeded but startup failed
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if stopErr := service.Stop(shutdownCtx); stopErr != nil {
			log.ErrorCtx(ctx, "failed to cleanup service after startup failure", stopErr)
		}

		os.Exit(1)
	}

	log.InfoContext(ctx, "service started successfully, waiting for shutdown signal")

	// Wait for shutdown signal - this blocks until SIGINT/SIGTERM is received
	// The service handles signal registration and graceful shutdown internally
	service.WaitForShutdown()

	log.InfoContext(ctx, "main process exiting")
}
