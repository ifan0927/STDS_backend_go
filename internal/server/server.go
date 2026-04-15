package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"stds_backend/internal/config"
	"stds_backend/internal/http/router"
	"stds_backend/internal/platform/database"
)

type Server struct {
	httpServer *http.Server
	db         *sql.DB
}

func New(cfg *config.Config) (*Server, error) {
	db, err := database.Open(context.Background(), cfg.DB.URL)
	if err != nil {
		return nil, err
	}

	engine := router.New(cfg.App)

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.App.Address(),
			Handler:      engine,
			ReadTimeout:  cfg.App.ReadTimeout,
			WriteTimeout: cfg.App.WriteTimeout,
		},
		db: db,
	}, nil
}

func (s *Server) Run() error {
	defer s.db.Close()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}

	return nil
}
