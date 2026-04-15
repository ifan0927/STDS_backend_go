package logging

import (
	"log/slog"
	"os"

	"stds_backend/internal/config"
)

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
