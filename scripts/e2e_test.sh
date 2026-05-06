#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

export E2E_BASE_URL="${E2E_BASE_URL:-http://127.0.0.1:8080}"
export E2E_DATABASE_URL="${E2E_DATABASE_URL:-postgres://stds:stds@localhost:5432/stds_backend_e2e?sslmode=disable}"
export E2E_FIREBASE_PROJECT_ID="${E2E_FIREBASE_PROJECT_ID:-demo-stds-backend}"
export E2E_FIREBASE_AUTH_EMULATOR_HOST="${E2E_FIREBASE_AUTH_EMULATOR_HOST:-127.0.0.1:9099}"
export E2E_TEST_EMAIL="${E2E_TEST_EMAIL:-e2e-admin@example.com}"
export E2E_TEST_PASSWORD="${E2E_TEST_PASSWORD:-Test123!}"
export E2E_SCHEDULER_KEY="${E2E_SCHEDULER_KEY:-e2e-scheduler-key}"

cd "$REPO_ROOT"
go test -tags=e2e ./test/e2e "$@"
