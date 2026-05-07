#!/usr/bin/env bash
set -euo pipefail

export LEGACY_E2E_BASE_URL="${LEGACY_E2E_BASE_URL:-http://127.0.0.1:8080}"
export LEGACY_E2E_DATABASE_URL="${LEGACY_E2E_DATABASE_URL:-postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable}"
export LEGACY_E2E_FIREBASE_PROJECT_ID="${LEGACY_E2E_FIREBASE_PROJECT_ID:-demo-stds-backend}"
export LEGACY_E2E_FIREBASE_AUTH_EMULATOR_HOST="${LEGACY_E2E_FIREBASE_AUTH_EMULATOR_HOST:-127.0.0.1:9099}"
export LEGACY_E2E_TEST_EMAIL="${LEGACY_E2E_TEST_EMAIL:-legacy-e2e-admin@example.com}"
export LEGACY_E2E_TEST_PASSWORD="${LEGACY_E2E_TEST_PASSWORD:-Test123!}"
export LEGACY_E2E_VALIDATION_REPORT_PATH="${LEGACY_E2E_VALIDATION_REPORT_PATH:-artifacts/legacy_migration/task13_validation_report.json}"

if [[ "$LEGACY_E2E_VALIDATION_REPORT_PATH" != /* ]]; then
  export LEGACY_E2E_VALIDATION_REPORT_PATH="$PWD/$LEGACY_E2E_VALIDATION_REPORT_PATH"
fi

go test -tags=legacye2e ./test/legacye2e "$@"
