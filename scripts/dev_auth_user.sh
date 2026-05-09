#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

export FIREBASE_PROJECT_ID="${FIREBASE_PROJECT_ID:-demo-stds-backend}"
export FIREBASE_AUTH_EMULATOR_HOST="${FIREBASE_AUTH_EMULATOR_HOST:-127.0.0.1:9099}"
export FIREBASE_TEST_UID="${FIREBASE_TEST_UID:-72sjYQv3GoNts3glUeiXmvbuBUx1}"

cd "$REPO_ROOT"
go run ./cmd/auth-emulator bootstrap-dev-user "$@"
