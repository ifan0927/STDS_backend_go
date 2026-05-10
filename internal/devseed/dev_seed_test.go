package devseed

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSafety(t *testing.T) {
	t.Run("accepts local development database", func(t *testing.T) {
		err := validateSafety(Options{
			AppEnv:      "local",
			DatabaseURL: "postgres://stds:stds@localhost:5432/stds_backend?sslmode=disable",
		})
		if err != nil {
			t.Fatalf("validateSafety returned error: %v", err)
		}
	})

	t.Run("rejects production app env", func(t *testing.T) {
		err := validateSafety(Options{
			AppEnv:      "production",
			DatabaseURL: "postgres://stds:stds@localhost:5432/stds_backend?sslmode=disable",
		})
		if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
			t.Fatalf("expected APP_ENV rejection, got %v", err)
		}
	})

	t.Run("rejects production-like database name", func(t *testing.T) {
		err := validateSafety(Options{
			AppEnv:      "local",
			DatabaseURL: "postgres://stds:stds@localhost:5432/stds_backend_prod?sslmode=disable",
		})
		if err == nil || !strings.Contains(err.Error(), "production-like") {
			t.Fatalf("expected production-like database rejection, got %v", err)
		}
	})

	t.Run("rejects unrelated database name", func(t *testing.T) {
		err := validateSafety(Options{
			AppEnv:      "test",
			DatabaseURL: "postgres://stds:stds@localhost:5432/customer_data?sslmode=disable",
		})
		if err == nil || !strings.Contains(err.Error(), "database") {
			t.Fatalf("expected database name rejection, got %v", err)
		}
	})
}

func TestIsReportCurrent(t *testing.T) {
	t.Run("uses live report for current target month in Taipei", func(t *testing.T) {
		now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
		if !isReportCurrent(now) {
			t.Fatal("expected 2026-05 to be current")
		}
	})

	t.Run("uses snapshot report after target month", func(t *testing.T) {
		now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
		if isReportCurrent(now) {
			t.Fatal("expected 2026-05 to be historical")
		}
	})
}
