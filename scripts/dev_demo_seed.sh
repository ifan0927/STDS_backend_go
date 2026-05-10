#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

export APP_ENV="${APP_ENV:-local}"

cd "$REPO_ROOT"
go run ./cmd/dev_seed frontend-demo "$@"
