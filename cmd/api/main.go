package main

import (
	"log/slog"
	"os"

	"stds_backend/internal/config"
	"stds_backend/internal/platform/logging"
	"stds_backend/internal/server"
)

func main() {
	bootstrapLogger := logging.NewBootstrap()

	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("startup failed", slog.String("stage", "load_config"), slog.String("cause", err.Error()))
		os.Exit(1)
	}

	logger := logging.New(cfg.App)
	app, err := server.New(cfg)
	if err != nil {
		logger.Error("startup failed", slog.String("stage", "create_server"), slog.String("cause", err.Error()))
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		logger.Error("server stopped with error", slog.String("stage", "run_server"), slog.String("cause", err.Error()))
		os.Exit(1)
	}
}
