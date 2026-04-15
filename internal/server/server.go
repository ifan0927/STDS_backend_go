package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"stds_backend/internal/config"
	"stds_backend/internal/http/router"
	"stds_backend/internal/platform/database"
	dbusers "stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/platform/logging"
)

type Server struct {
	httpServer *http.Server
	db         *sql.DB
	logger     *slog.Logger
}

func New(cfg *config.Config) (*Server, error) {
	db, err := database.Open(context.Background(), cfg.DB.URL)
	if err != nil {
		return nil, err
	}

	logger := logging.New(cfg.App)

	authenticator, err := platformfirebase.New(context.Background(), cfg.Firebase)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	userRepo := dbusers.NewRepository(db)

	engine := router.New(cfg.App, logger, db, authenticator, userRepo)

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.App.Address(),
			Handler:      engine,
			ReadTimeout:  cfg.App.ReadTimeout,
			WriteTimeout: cfg.App.WriteTimeout,
		},
		db:     db,
		logger: logger,
	}, nil
}

func (s *Server) Run() error {
	defer s.db.Close()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}

	return nil
}
