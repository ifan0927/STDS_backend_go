//go:build legacye2e

package legacye2e

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

type legacyE2EConfig struct {
	BaseURL              string
	DatabaseURL          string
	FirebaseProjectID    string
	FirebaseEmulatorHost string
	TestEmail            string
	TestPassword         string
	ValidationReportPath string
}

func loadLegacyE2EConfig() (legacyE2EConfig, error) {
	cfg := legacyE2EConfig{
		BaseURL:              strings.TrimRight(os.Getenv("LEGACY_E2E_BASE_URL"), "/"),
		DatabaseURL:          os.Getenv("LEGACY_E2E_DATABASE_URL"),
		FirebaseProjectID:    os.Getenv("LEGACY_E2E_FIREBASE_PROJECT_ID"),
		FirebaseEmulatorHost: os.Getenv("LEGACY_E2E_FIREBASE_AUTH_EMULATOR_HOST"),
		TestEmail:            os.Getenv("LEGACY_E2E_TEST_EMAIL"),
		TestPassword:         os.Getenv("LEGACY_E2E_TEST_PASSWORD"),
		ValidationReportPath: os.Getenv("LEGACY_E2E_VALIDATION_REPORT_PATH"),
	}

	missing := make([]string, 0)
	required := map[string]string{
		"LEGACY_E2E_BASE_URL":                    cfg.BaseURL,
		"LEGACY_E2E_DATABASE_URL":                cfg.DatabaseURL,
		"LEGACY_E2E_FIREBASE_PROJECT_ID":         cfg.FirebaseProjectID,
		"LEGACY_E2E_FIREBASE_AUTH_EMULATOR_HOST": cfg.FirebaseEmulatorHost,
		"LEGACY_E2E_TEST_EMAIL":                  cfg.TestEmail,
		"LEGACY_E2E_TEST_PASSWORD":               cfg.TestPassword,
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return legacyE2EConfig{}, fmt.Errorf("missing required legacy E2E environment variables: %s", strings.Join(missing, ", "))
	}

	if err := requireLegacyE2EDatabaseName(cfg.DatabaseURL); err != nil {
		return legacyE2EConfig{}, err
	}

	return cfg, nil
}

func requireLegacyE2EDatabaseName(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse LEGACY_E2E_DATABASE_URL: %w", err)
	}

	dbName := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if dbName == "" {
		return errors.New("LEGACY_E2E_DATABASE_URL must include a database name")
	}

	normalized := strings.ToLower(dbName)
	if !strings.Contains(normalized, "legacy") {
		return fmt.Errorf("refusing legacy E2E database %q: database name must contain legacy", dbName)
	}
	if !strings.Contains(normalized, "e2e") && !strings.Contains(normalized, "test") && !strings.Contains(normalized, "staging") {
		return fmt.Errorf("refusing legacy E2E database %q: database name must contain e2e, test, or staging", dbName)
	}

	return nil
}
