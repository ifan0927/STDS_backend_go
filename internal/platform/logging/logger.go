package logging

import (
	"log/slog"
	"os"

	"stds_backend/internal/config"
)

// NewBootstrap builds a JSON logger for startup and bootstrap failures before
// application config has been fully loaded.
func NewBootstrap() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// New builds the application logger with environment-specific log level
// defaults.
func New(appCfg config.AppConfig) *slog.Logger {
	level := slog.LevelInfo
	if appCfg.Env == "local" {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler).With(
		slog.String("service", appCfg.Name),
		slog.String("env", appCfg.Env),
	)
}
