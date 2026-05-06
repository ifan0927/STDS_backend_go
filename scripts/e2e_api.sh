#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

export APP_ENV="${APP_ENV:-e2e}"
export APP_HOST="${APP_HOST:-127.0.0.1}"
export APP_PORT="${APP_PORT:-8080}"
export APP_SCHEDULER_KEY="${APP_SCHEDULER_KEY:-e2e-scheduler-key}"
export DATABASE_URL="${DATABASE_URL:-postgres://stds:stds@localhost:5432/stds_backend_e2e?sslmode=disable}"
export FIREBASE_PROJECT_ID="${FIREBASE_PROJECT_ID:-demo-stds-backend}"
export FIREBASE_AUTH_EMULATOR_HOST="${FIREBASE_AUTH_EMULATOR_HOST:-127.0.0.1:9099}"
export GCS_BUCKET_NAME="${GCS_BUCKET_NAME:-stds-e2e}"
export STORAGE_EMULATOR_HOST="${STORAGE_EMULATOR_HOST:-http://127.0.0.1:4443}"
export RESEND_API_KEY="${RESEND_API_KEY:-e2e-resend-key}"
export RESEND_FROM_EMAIL="${RESEND_FROM_EMAIL:-e2e@example.com}"

cd "$REPO_ROOT"
go run ./cmd/api
