# Local Operations

Operational details for local development, E2E verification, environment
configuration, CI command parity, and legacy migration planning.

## Firebase Auth Emulator

Use the Auth Emulator for local backend development. In emulator mode, the backend still verifies Firebase ID tokens, but it does not require a service account JSON file.

1. Set `.env`:

```bash
FIREBASE_PROJECT_ID=demo-stds-backend
FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9099
```

2. Start the Auth Emulator with any Firebase project alias you use locally.
```bash
firebase emulators:start --only auth
```

3. Create or update the local emulator user and matching backend DB user:

```bash
scripts/dev_auth_user.sh
```

By default this creates an `admin` backend user for `testuser@example.com` with
password `Test123!` in the database pointed at by `DATABASE_URL`. The database
must already exist and have migrations applied. You can override the local
account fields:

```bash
scripts/dev_auth_user.sh \
  --uid 72sjYQv3GoNts3glUeiXmvbuBUx1 \
  --email testuser@example.com \
  --password 'Test123!' \
  --name 'Local Admin' \
  --role admin
```

To create one local account per backend role for frontend testing:

```bash
scripts/dev_auth_users.sh
```

This creates or updates:

| Email | Password | Role |
| --- | --- | --- |
| `local-admin@example.com` | `Test123!` | `admin` |
| `local-organizer@example.com` | `Test123!` | `organizer` |
| `local-staff@example.com` | `Test123!` | `staff` |
| `local-owner@example.com` | `Test123!` | `owner` |

Set `DEV_AUTH_PASSWORD` to override the shared local password.

4. Seed deterministic frontend demo data when the UI needs non-empty local
   screens:

```bash
scripts/dev_demo_seed.sh
```

This opt-in seed refuses production-like environments and creates a rerunnable
demo property with rooms, tenants, leases, bills, meter history, reports,
journal, repair, and checkout data. It is local frontend support data and is
separate from schema migrations and legacy migration validation.

5. Create or repair the local brand readonly database user when developing the
   future brand thin backend boundary:

```bash
scripts/dev_brand_readonly.sh
```

This local helper uses `DATABASE_URL` as the admin connection by default,
creates or repairs `brand_readonly`, grants only the approved brand views, and
verifies representative core base tables are not selectable. Override
`BRAND_READONLY_PASSWORD`, `BRAND_READONLY_ADMIN_DATABASE_URL`, or
`BRAND_READONLY_DATABASE_URL` when your local database connection differs from
the Docker defaults.

6. Get an ID token from the emulator:

```bash
go run ./cmd/auth-emulator issue-token \
  --email testuser@example.com \
  --password 'Test123!'
```

7. Use the returned token to call a protected route:

```bash
curl -i \
  -H "Authorization: Bearer <ID_TOKEN>" \
  http://127.0.0.1:8080/api/v1/authz-test/properties/property-1
```

If the `users` table contains the same `firebase_uid`, the request will pass auth and resolve the DB user.

## E2E tests

The E2E suite is opt-in and uses Go `testing` with the `e2e` build tag. It calls an already-running API through HTTP, resets a dedicated PostgreSQL test database schema, runs migrations, seeds the backend user row, and obtains a real bearer token from the Firebase Auth Emulator.

Use a separate database name for E2E, for example `stds_backend_e2e`. This can live in the same local PostgreSQL container as the normal development database; `docker-compose.yml` does not need a second PostgreSQL service.

Start dependencies:

```bash
docker compose up -d postgres
docker exec stds-postgres psql -U stds -d postgres -c "CREATE DATABASE stds_backend_e2e"
firebase emulators:start --only auth
```

Start the API against the E2E database:

```bash
./scripts/e2e_api.sh
```

Run the E2E suite:

```bash
./scripts/e2e_test.sh
```

The harness refuses to reset a database unless the database name contains `e2e` or `test`.
The local scripts provide defaults for E2E environment variables and still allow overrides, for example `APP_PORT=8081 E2E_BASE_URL=http://127.0.0.1:8081 ./scripts/e2e_test.sh`.

## Legacy migrated E2E sign-off

Legacy migrated data checks are separate from the normal seed-based E2E suite.
They are intended for one-time legacy migration sign-off and are not wired into
required PR CI.

Use a separate database name for this mode, for example
`stds_backend_legacy_e2e`. The legacy E2E tests do not reset the database or
seed business data. They only seed the backend admin user row needed for the
Firebase Auth Emulator token, then discover representative migrated records
through legacy mapping tables.

Prepare the legacy E2E database:

```bash
docker compose up -d postgres
docker exec stds-postgres psql -U stds -d postgres -c "CREATE DATABASE stds_backend_legacy_e2e"
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate up
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy properties
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy rooms
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy tenants
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy leases
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy room-status
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy bills
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy journal
DATABASE_URL=postgres://stds:stds@localhost:5432/stds_backend_legacy_e2e?sslmode=disable go run ./cmd/migrate_legacy validate
```

Before sign-off, review the generated validation report and source/target
reconciliation. By default, `scripts/legacy_e2e_test.sh` requires
`artifacts/legacy_migration/task13_validation_report.json` to exist.

Start dependencies and the API:

```bash
firebase emulators:start --only auth
./scripts/legacy_e2e_api.sh
```

Run the legacy read checks:

```bash
./scripts/legacy_e2e_test.sh
```

## Email notifications

`POST /api/v1/users` now creates the Firebase Auth user, generates a Firebase password reset URL, and sends the onboarding email through Resend.

Required environment variables:

```bash
RESEND_API_KEY=
RESEND_FROM_EMAIL=onboarding@example.com
RESEND_FROM_NAME=STDS
FIREBASE_PASSWORD_RESET_REDIRECT_URL=
```

`FIREBASE_PASSWORD_RESET_REDIRECT_URL` is optional. When set, Firebase will embed it in the generated password reset link as the continue URL.

## Attachment storage

Attachment upload URLs use Google Cloud Storage. Local unit tests should inject a fake storage port and must not require real GCS credentials.

Required environment variables for real GCS-backed attachment uploads:

```bash
GCS_BUCKET_NAME=
GCS_SIGNED_URL_TTL=15m
```

Optional local emulator setting:

```bash
STORAGE_EMULATOR_HOST=http://127.0.0.1:4443
```

E2E attachment acceptance uses metadata-only storage by default:

```bash
ATTACHMENT_STORAGE_MODE=fake-metadata
```

This mode is only allowed when `APP_ENV` is `e2e` or `test`. It returns a fake
upload URL and valid object metadata so E2E tests can cover upload-token,
registration, read, and authorization behavior without production GCS or a
storage emulator.

Production or staging environments must provide Google Application Default Credentials, for example through `GOOGLE_APPLICATION_CREDENTIALS` or the runtime service account. The service account needs permission to create signed upload URLs and read object metadata for registration checks.

## CI checks

The PR CI gate runs these checks for pull requests targeting `dev` and pushes to `dev`:

```bash
go test ./...
go build -o /tmp/stds-api ./cmd/api
npm ci
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
npm run openapi:lint
npm run openapi:generate
git diff --exit-code -- docs/spec/openapi.yaml internal/http/api/openapi.gen.go
docker exec stds-postgres psql -U stds -d postgres -c "DROP DATABASE IF EXISTS ci_migration_smoke"
docker exec stds-postgres psql -U stds -d postgres -c "CREATE DATABASE ci_migration_smoke"
DATABASE_URL=postgres://stds:stds@localhost:5432/ci_migration_smoke?sslmode=disable go run ./cmd/migrate up
docker exec stds-postgres psql -U stds -d postgres -c "DROP DATABASE IF EXISTS ci_migration_smoke"
go test -tags=e2e ./test/e2e
```

## Database migrations

- Run all pending migrations: `go run ./cmd/migrate up`
- Roll back applied migrations in reverse order: `go run ./cmd/migrate down`

## Brand readonly database access

Demo brand frontend data is exposed to a future thin backend through approved
PostgreSQL views only. The core backend migrations create the views; deployment
or DBA provisioning must create the Cloud SQL readonly principal and grant only
the approved view access.

The current approved views are:

- `approved_brand_profile_v1`
- `approved_brand_faq_items_v1`
- `approved_brand_property_availability_v1`

The brand readonly principal should receive `USAGE` on the schema and `SELECT`
on those views only. Do not grant it direct access to base tables such as
`properties`, `rooms`, `brand_profiles`, `brand_faq_items`, `tenants`, `leases`,
`bills`, `attachments`, or deposit/accounting tables. Local PostgreSQL contract
tests use an ephemeral equivalent role to verify that the approved views are
selectable while base tables are not.

For local thin-backend development, the expected order is:

```bash
docker compose up -d postgres
go run ./cmd/migrate up
scripts/dev_demo_seed.sh
scripts/dev_brand_readonly.sh
```

The local readonly connection string defaults to:

```bash
BRAND_READONLY_DATABASE_URL=postgres://brand_readonly:brand_readonly@localhost:5432/stds_backend?sslmode=disable
```

Staging and production must provision the equivalent Cloud SQL principal outside
application migrations. The deployment or DBA workflow should create the
dedicated readonly principal, grant schema `USAGE`, grant `SELECT` only on the
approved views, store the thin-backend credential through the environment or
Secret Manager, and run a deployed smoke check that mirrors the local approved
view and base-table denial checks.

## Legacy migration planning

- Validate the checked-in legacy JSON exports and generate the Task 5 architecture report: `go run ./cmd/migrate_legacy plan`
- Override source/report directories when needed: `go run ./cmd/migrate_legacy plan --source-dir docs/mirgations --report-dir artifacts/legacy_migration`
