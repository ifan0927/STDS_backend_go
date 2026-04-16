package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config groups all runtime configuration consumed by the application.
type Config struct {
	App      AppConfig
	DB       DatabaseConfig
	Firebase FirebaseConfig
}

// AppConfig contains HTTP service settings and process metadata.
type AppConfig struct {
	Name                string
	Env                 string
	Host                string
	Port                string
	BaseURL             string
	SchedulerKey        string
	SchedulerJobTimeout time.Duration
	SchedulerMaxRetries int
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	URL string
}

// FirebaseConfig contains Firebase authentication client settings.
type FirebaseConfig struct {
	ProjectID        string
	CredentialsFile  string
	AuthEmulatorHost string
	TestUID          string
}

// UsesAuthEmulator reports whether Firebase auth calls should target the local
// emulator.
func (c FirebaseConfig) UsesAuthEmulator() bool {
	return c.AuthEmulatorHost != ""
}

// Load reads environment-backed configuration and validates required values.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Name:                getEnv("APP_NAME", "stds-backend"),
			Env:                 getEnv("APP_ENV", "local"),
			Host:                getEnv("APP_HOST", "0.0.0.0"),
			Port:                getEnv("APP_PORT", "8080"),
			BaseURL:             getEnv("APP_BASE_URL", "http://localhost:8080"),
			SchedulerKey:        os.Getenv("APP_SCHEDULER_KEY"),
			SchedulerJobTimeout: getDurationEnv("APP_SCHEDULER_JOB_TIMEOUT", 2*time.Minute),
			SchedulerMaxRetries: getIntEnv("APP_SCHEDULER_MAX_RETRIES", 3),
			ReadTimeout:         getDurationEnv("APP_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:        getDurationEnv("APP_WRITE_TIMEOUT", 10*time.Second),
		},
		DB: DatabaseConfig{
			URL: os.Getenv("DATABASE_URL"),
		},
		Firebase: FirebaseConfig{
			ProjectID:        os.Getenv("FIREBASE_PROJECT_ID"),
			CredentialsFile:  os.Getenv("FIREBASE_CREDENTIALS_FILE"),
			AuthEmulatorHost: os.Getenv("FIREBASE_AUTH_EMULATOR_HOST"),
			TestUID:          os.Getenv("FIREBASE_TEST_UID"),
		},
	}

	if cfg.DB.URL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	return cfg, nil
}

// Address returns the host:port pair used by the HTTP server.
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

func getIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}
