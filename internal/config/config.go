package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App AppConfig
	DB  DatabaseConfig
}

type AppConfig struct {
	Name         string
	Env          string
	Host         string
	Port         string
	BaseURL      string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type DatabaseConfig struct {
	URL string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Name:         getEnv("APP_NAME", "stds-backend"),
			Env:          getEnv("APP_ENV", "local"),
			Host:         getEnv("APP_HOST", "0.0.0.0"),
			Port:         getEnv("APP_PORT", "8080"),
			BaseURL:      getEnv("APP_BASE_URL", "http://localhost:8080"),
			ReadTimeout:  getDurationEnv("APP_READ_TIMEOUT", 5*time.Second),
			WriteTimeout: getDurationEnv("APP_WRITE_TIMEOUT", 10*time.Second),
		},
		DB: DatabaseConfig{
			URL: os.Getenv("DATABASE_URL"),
		},
	}

	if cfg.DB.URL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	return cfg, nil
}

func (a AppConfig) Address() string {
	return fmt.Sprintf("%s:%s", a.Host, a.Port)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return duration
}
