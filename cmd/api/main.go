package main

import (
	"log"

	"stds_backend/internal/config"
	"stds_backend/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	app, err := server.New(cfg)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	if err := app.Run(); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
