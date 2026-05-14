package main

import (
	"context"
	"log"
	"os"

	"stds_backend/internal/config"
	"stds_backend/internal/platform/database"
	"stds_backend/internal/platform/database/migrate"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: go run ./cmd/migrate [up|down]")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, database.PoolConfig{
		MaxOpenConns:    cfg.DB.MaxOpenConns,
		MaxIdleConns:    cfg.DB.MaxIdleConns,
		ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
	})
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	runner := migrate.NewRunner(db)

	switch os.Args[1] {
	case "up":
		err = runner.Up(context.Background())
	case "down":
		err = runner.Down(context.Background())
	default:
		log.Fatalf("unsupported direction %q", os.Args[1])
	}

	if err != nil {
		log.Fatalf("run migrations: %v", err)
	}
}
