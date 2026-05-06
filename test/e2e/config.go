//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

type e2eConfig struct {
	BaseURL              string
	DatabaseURL          string
	FirebaseProjectID    string
	FirebaseEmulatorHost string
	TestEmail            string
	TestPassword         string
	SchedulerKey         string
}

func loadE2EConfig() (e2eConfig, error) {
	cfg := e2eConfig{
		BaseURL:              strings.TrimRight(os.Getenv("E2E_BASE_URL"), "/"),
		DatabaseURL:          os.Getenv("E2E_DATABASE_URL"),
		FirebaseProjectID:    os.Getenv("E2E_FIREBASE_PROJECT_ID"),
		FirebaseEmulatorHost: os.Getenv("E2E_FIREBASE_AUTH_EMULATOR_HOST"),
		TestEmail:            os.Getenv("E2E_TEST_EMAIL"),
		TestPassword:         os.Getenv("E2E_TEST_PASSWORD"),
		SchedulerKey:         os.Getenv("E2E_SCHEDULER_KEY"),
	}

	missing := make([]string, 0)
	required := map[string]string{
		"E2E_BASE_URL":                    cfg.BaseURL,
		"E2E_DATABASE_URL":                cfg.DatabaseURL,
		"E2E_FIREBASE_PROJECT_ID":         cfg.FirebaseProjectID,
		"E2E_FIREBASE_AUTH_EMULATOR_HOST": cfg.FirebaseEmulatorHost,
		"E2E_TEST_EMAIL":                  cfg.TestEmail,
		"E2E_TEST_PASSWORD":               cfg.TestPassword,
		"E2E_SCHEDULER_KEY":               cfg.SchedulerKey,
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return e2eConfig{}, fmt.Errorf("missing required E2E environment variables: %s", strings.Join(missing, ", "))
	}

	if err := requireE2EDatabaseName(cfg.DatabaseURL); err != nil {
		return e2eConfig{}, err
	}

	return cfg, nil
}

func requireE2EDatabaseName(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse E2E_DATABASE_URL: %w", err)
	}

	dbName := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if dbName == "" {
		return errors.New("E2E_DATABASE_URL must include a database name")
	}

	normalized := strings.ToLower(dbName)
	if !strings.Contains(normalized, "e2e") && !strings.Contains(normalized, "test") {
		return fmt.Errorf("refusing to reset database %q: E2E database name must contain e2e or test", dbName)
	}

	return nil
}
