#!/usr/bin/env bash
set -euo pipefail

export APP_ENV="${APP_ENV:-e2e}"
export APP_HOST="${APP_HOST:-127.0.0.1}"
export APP_PORT="${APP_PORT:-8080}"
export APP_SCHEDULER_KEY="${APP_SCHEDULER_KEY:-legacy-e2e-scheduler-key}"
export DATABASE_URL="${DATABASE_URL:-postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable}"
export FIREBASE_PROJECT_ID="${FIREBASE_PROJECT_ID:-demo-stds-backend}"
export FIREBASE_AUTH_EMULATOR_HOST="${FIREBASE_AUTH_EMULATOR_HOST:-127.0.0.1:9099}"
export GCS_BUCKET_NAME="${GCS_BUCKET_NAME:-stds-legacy-e2e}"
export ATTACHMENT_STORAGE_MODE="${ATTACHMENT_STORAGE_MODE:-fake-metadata}"
export RESEND_API_KEY="${RESEND_API_KEY:-legacy-e2e-resend-key}"
export RESEND_FROM_EMAIL="${RESEND_FROM_EMAIL:-legacy-e2e@example.com}"

go run ./cmd/api
