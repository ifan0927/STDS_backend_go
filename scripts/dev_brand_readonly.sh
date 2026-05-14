#!/usr/bin/env sh

set -eu

ADMIN_DATABASE_URL="${BRAND_READONLY_ADMIN_DATABASE_URL:-${DATABASE_URL:-postgres://stds:stds@localhost:5432/stds_backend?sslmode=disable}}"
READONLY_PASSWORD="${BRAND_READONLY_PASSWORD:-brand_readonly}"
READONLY_DATABASE_URL="${BRAND_READONLY_DATABASE_URL:-postgres://brand_readonly:${READONLY_PASSWORD}@localhost:5432/stds_backend?sslmode=disable}"

psql_cmd() {
  if command -v psql >/dev/null 2>&1; then
    psql "$@"
    return
  fi

  if command -v docker >/dev/null 2>&1 && docker ps --filter name=stds-postgres --format '{{.Names}}' 2>/dev/null | grep -qx stds-postgres; then
    docker exec -i stds-postgres psql "$@"
    return
  fi

  echo "psql is required, or the stds-postgres Docker container must be running" >&2
  exit 1
}

psql_cmd "$ADMIN_DATABASE_URL" -v ON_ERROR_STOP=1 -v readonly_password="$READONLY_PASSWORD" <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'brand_readonly') THEN
    CREATE ROLE brand_readonly LOGIN;
  END IF;
END
$$;

ALTER ROLE brand_readonly WITH LOGIN PASSWORD :'readonly_password';
REVOKE ALL PRIVILEGES ON SCHEMA public FROM brand_readonly;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM brand_readonly;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM brand_readonly;
GRANT USAGE ON SCHEMA public TO brand_readonly;
GRANT SELECT ON public.approved_brand_profile_v1 TO brand_readonly;
GRANT SELECT ON public.approved_brand_faq_items_v1 TO brand_readonly;
GRANT SELECT ON public.approved_brand_property_availability_v1 TO brand_readonly;
SQL

check_can_select() {
  view_name="$1"
  psql_cmd "$READONLY_DATABASE_URL" -v ON_ERROR_STOP=1 -c "SELECT * FROM public.${view_name} LIMIT 1" >/dev/null
}

check_cannot_select() {
  table_name="$1"
  if psql_cmd "$READONLY_DATABASE_URL" -v ON_ERROR_STOP=1 -c "SELECT * FROM public.${table_name} LIMIT 1" >/dev/null 2>&1; then
    echo "brand_readonly unexpectedly selected base table: ${table_name}" >&2
    exit 1
  fi
}

check_can_select approved_brand_profile_v1
check_can_select approved_brand_faq_items_v1
check_can_select approved_brand_property_availability_v1

check_cannot_select properties
check_cannot_select rooms
check_cannot_select brand_profiles
check_cannot_select brand_faq_items

echo "brand_readonly can select approved views and cannot select representative base tables"
