#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

LEGACY_SOURCE_DIR="${LEGACY_SOURCE_DIR:-docs/mirgations}"
LEGACY_REPORT_DIR="${LEGACY_REPORT_DIR:-artifacts/legacy_migration}"
DB_DUMP_DIR="${DB_DUMP_DIR:-artifacts/db_dumps}"
DB_DUMP_NAME="${DB_DUMP_NAME:-legacy_db_dump_$(date +%Y%m%d_%H%M%S).sql}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-stds-postgres}"
POSTGRES_ADMIN_USER="${POSTGRES_ADMIN_USER:-stds}"
TEMP_DB_HOST="${TEMP_DB_HOST:-127.0.0.1}"
RUN_ID="$(date +%Y%m%d_%H%M%S)_$$"
TEMP_DB_ROLE="${TEMP_DB_ROLE:-${TEMP_DB_USER:-stds_legacy_dump_$RUN_ID}}"
TEMP_DB_NAME="${TEMP_DB_NAME:-stds_legacy_dump_$RUN_ID}"
KEEP_TEMP_DB="${KEEP_TEMP_DB:-0}"
INCLUDE_ADMIN_USER="${INCLUDE_ADMIN_USER:-0}"

find_tool() {
  tool_name="$1"
  fallback_path="$2"

  if command -v "$tool_name" >/dev/null 2>&1; then
    command -v "$tool_name"
    return 0
  fi

  if [ -x "$fallback_path" ]; then
    printf '%s\n' "$fallback_path"
    return 0
  fi

  printf 'missing required tool: %s\n' "$tool_name" >&2
  printf 'install PostgreSQL client tools or add %s to PATH\n' "$(dirname "$fallback_path")" >&2
  return 1
}

run_legacy_stage() {
  stage="$1"

  if [ "$stage" = "room-status" ]; then
    "$GO_BIN" run ./cmd/migrate_legacy "$stage" \
      --report-dir "$LEGACY_REPORT_DIR"
    return 0
  fi

  "$GO_BIN" run ./cmd/migrate_legacy "$stage" \
    --source-dir "$LEGACY_SOURCE_DIR" \
    --report-dir "$LEGACY_REPORT_DIR"
}

validate_identifier() {
  value="$1"
  label="$2"

  case "$value" in
    ""|[!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_]*|*[!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789]*)
      printf '%s must be a PostgreSQL identifier using letters, numbers, and underscores, starting with a letter or underscore: %s\n' "$label" "$value" >&2
      return 1
      ;;
  esac
}

generate_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
    return 0
  fi

  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
    return 0
  fi

  printf 'missing required tool: openssl or uuidgen\n' >&2
  return 1
}

admin_psql() {
  "$DOCKER_BIN" exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_ADMIN_USER" -d postgres "$@"
}

cleanup_temp_db() {
  if [ "$KEEP_TEMP_DB" = "1" ]; then
    printf '\nKeeping temporary database for debugging: %s\n' "$TEMP_DB_NAME"
    printf 'Temporary DB role/user: %s\n' "$TEMP_DB_ROLE"
    return 0
  fi

  if [ "${TEMP_DB_CREATED:-0}" = "1" ]; then
    printf '\nRemoving temporary database and role: %s / %s\n' "$TEMP_DB_NAME" "$TEMP_DB_ROLE"
    admin_psql -v ON_ERROR_STOP=1 \
      -v temp_db="$TEMP_DB_NAME" \
      -v temp_role="$TEMP_DB_ROLE" <<'SQL' >/dev/null 2>&1 || true
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = :'temp_db'
  AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS :"temp_db" WITH (FORCE);
DROP ROLE IF EXISTS :"temp_role";
SQL
  fi
}

bootstrap_admin_user() {
  if [ "$INCLUDE_ADMIN_USER" != "1" ]; then
    return 0
  fi

  : "${ADMIN_FIREBASE_UID:?ADMIN_FIREBASE_UID is required when INCLUDE_ADMIN_USER=1}"
  : "${ADMIN_EMAIL:?ADMIN_EMAIL is required when INCLUDE_ADMIN_USER=1}"
  : "${ADMIN_NAME:?ADMIN_NAME is required when INCLUDE_ADMIN_USER=1}"

  printf '\nUpserting opt-in admin user row before dump...\n'
  "$PSQL_BIN" "$DATABASE_URL" \
    -v ON_ERROR_STOP=1 \
    -v firebase_uid="$ADMIN_FIREBASE_UID" \
    -v admin_email="$ADMIN_EMAIL" \
    -v admin_name="$ADMIN_NAME" <<'SQL'
INSERT INTO users (
    firebase_uid,
    email,
    name,
    role,
    permission_overrides,
    assigned_property_ids
) VALUES (
    :'firebase_uid',
    :'admin_email',
    :'admin_name',
    'admin',
    '[]'::jsonb,
    '[]'::jsonb
)
ON CONFLICT (firebase_uid) WHERE deleted_at IS NULL
DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    role = EXCLUDED.role,
    permission_overrides = EXCLUDED.permission_overrides,
    assigned_property_ids = EXCLUDED.assigned_property_ids,
    updated_at = now(),
    version = users.version + 1
RETURNING id, firebase_uid, email, role, deleted_at IS NULL AS active;
SQL
}

PSQL_BIN=$(find_tool psql /opt/homebrew/opt/libpq/bin/psql)
PG_DUMP_BIN=$(find_tool pg_dump /opt/homebrew/opt/libpq/bin/pg_dump)
GO_BIN=$(find_tool go /opt/homebrew/bin/go)
DOCKER_BIN=$(find_tool docker /opt/homebrew/bin/docker)

cd "$REPO_ROOT"

validate_identifier "$TEMP_DB_ROLE" TEMP_DB_ROLE
validate_identifier "$TEMP_DB_NAME" TEMP_DB_NAME

if [ ! -d "$LEGACY_SOURCE_DIR" ]; then
  printf 'legacy source directory does not exist: %s\n' "$LEGACY_SOURCE_DIR" >&2
  exit 1
fi

if ! find "$LEGACY_SOURCE_DIR" -maxdepth 1 -type f -name '*.json' | grep -q .; then
  printf 'legacy source directory has no JSON exports: %s\n' "$LEGACY_SOURCE_DIR" >&2
  exit 1
fi

mkdir -p "$LEGACY_REPORT_DIR" "$DB_DUMP_DIR"

TEMP_DB_PASSWORD="${TEMP_DB_PASSWORD:-$(generate_password)}"

if [ "$("$DOCKER_BIN" inspect -f '{{.State.Running}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)" != "true" ]; then
  printf 'PostgreSQL container is not running: %s\n' "$POSTGRES_CONTAINER" >&2
  printf 'Start it first with: docker compose up -d postgres\n' >&2
  exit 1
fi

TEMP_DB_PORT="${TEMP_DB_PORT:-$("$DOCKER_BIN" port "$POSTGRES_CONTAINER" 5432/tcp | sed -n '1s/.*://p')}"
if [ -z "$TEMP_DB_PORT" ]; then
  printf 'could not determine host port for %s 5432/tcp; set TEMP_DB_PORT explicitly\n' "$POSTGRES_CONTAINER" >&2
  exit 1
fi

printf 'Creating temporary PostgreSQL role and database in container: %s\n' "$POSTGRES_CONTAINER"
admin_psql -v ON_ERROR_STOP=1 \
  -v temp_role="$TEMP_DB_ROLE" \
  -v temp_password="$TEMP_DB_PASSWORD" \
  -v temp_db="$TEMP_DB_NAME" <<'SQL'
CREATE ROLE :"temp_role" LOGIN PASSWORD :'temp_password';
CREATE DATABASE :"temp_db" OWNER :"temp_role";
SQL

TEMP_DB_CREATED=1
trap cleanup_temp_db EXIT INT TERM

DATABASE_URL="postgres://$TEMP_DB_ROLE:$TEMP_DB_PASSWORD@$TEMP_DB_HOST:$TEMP_DB_PORT/$TEMP_DB_NAME?sslmode=disable"
export DATABASE_URL

printf 'Waiting for temporary database to accept connections...\n'
ready_attempt=0
while ! "$PSQL_BIN" "$DATABASE_URL" -v ON_ERROR_STOP=1 -c "SELECT 1;" >/dev/null 2>&1; do
  ready_attempt=$((ready_attempt + 1))
  if [ "$ready_attempt" -ge 60 ]; then
    printf 'temporary PostgreSQL did not become ready in time\n' >&2
    exit 1
  fi
  sleep 1
done

printf 'Preparing legacy database dump.\n'
printf 'PostgreSQL container: %s\n' "$POSTGRES_CONTAINER"
printf 'Temporary DB role/user: %s\n' "$TEMP_DB_ROLE"
printf 'Temporary DB password: %s\n' "$TEMP_DB_PASSWORD"
printf 'Temporary DB name: %s\n' "$TEMP_DB_NAME"
printf 'Temporary DB host: %s\n' "$TEMP_DB_HOST"
printf 'Temporary DB port: %s\n' "$TEMP_DB_PORT"
printf 'Legacy source dir: %s\n' "$LEGACY_SOURCE_DIR"
printf 'Legacy report dir: %s\n' "$LEGACY_REPORT_DIR"
printf 'Dump output dir: %s\n' "$DB_DUMP_DIR"
printf 'Temporary DATABASE_URL is set internally for migration commands.\n'

printf '\nRunning schema migrations...\n'
"$GO_BIN" run ./cmd/migrate up

printf '\nRunning legacy migration stages...\n'
run_legacy_stage plan
run_legacy_stage properties
run_legacy_stage rooms
run_legacy_stage tenants
run_legacy_stage leases
run_legacy_stage room-status
run_legacy_stage bills
run_legacy_stage journal
run_legacy_stage validate

bootstrap_admin_user

printf '\nLatest schema migration version:\n'
"$PSQL_BIN" "$DATABASE_URL" -v ON_ERROR_STOP=1 -c \
  "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;"

printf '\nLegacy mapping table counts:\n'
"$PSQL_BIN" "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'legacy_property_mappings' AS table_name, COUNT(*) FROM legacy_property_mappings
UNION ALL
SELECT 'legacy_room_mappings', COUNT(*) FROM legacy_room_mappings
UNION ALL
SELECT 'legacy_tenant_mappings', COUNT(*) FROM legacy_tenant_mappings
UNION ALL
SELECT 'legacy_lease_mappings', COUNT(*) FROM legacy_lease_mappings
UNION ALL
SELECT 'legacy_bill_mappings', COUNT(*) FROM legacy_bill_mappings
UNION ALL
SELECT 'legacy_schedule_mappings', COUNT(*) FROM legacy_schedule_mappings
ORDER BY table_name;
SQL

DUMP_PATH="$DB_DUMP_DIR/$DB_DUMP_NAME"

printf '\nWriting plain SQL dump: %s\n' "$DUMP_PATH"
"$PG_DUMP_BIN" \
  --format=plain \
  --no-owner \
  --no-privileges \
  --file "$DUMP_PATH" \
  "$DATABASE_URL"

printf '\nLegacy database dump prepared: %s\n' "$DUMP_PATH"
printf 'Treat this dump as sensitive operational data. Do not commit or attach it to issues.\n'
