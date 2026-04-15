#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

if command -v oapi-codegen >/dev/null 2>&1; then
  OAPI_CODEGEN_BIN=$(command -v oapi-codegen)
else
  GOPATH_BIN=$(go env GOPATH)/bin/oapi-codegen
  if [ -x "$GOPATH_BIN" ]; then
    OAPI_CODEGEN_BIN=$GOPATH_BIN
  else
    echo "oapi-codegen is not installed. Run: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1" >&2
    exit 1
  fi
fi

cd "$REPO_ROOT/internal/http/api"

"$OAPI_CODEGEN_BIN" \
  -config cfg.yaml \
  "$REPO_ROOT/docs/spec/openapi.yaml"
