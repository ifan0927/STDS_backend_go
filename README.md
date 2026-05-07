# STDS Backend

Minimal backend scaffold for local development with Go, Gin, PostgreSQL, and Scalar API docs.

## Local development

1. Copy `.env.example` to `.env` if needed.
2. Start PostgreSQL:

```bash
docker compose up -d postgres
```

3. Apply database migrations:

```bash
go run ./cmd/migrate up
```

4. Run the API:

```bash
go run ./cmd/api
```

5. Open local docs:

- Scalar UI: `http://localhost:8080/scalar`
- OpenAPI file: `http://localhost:8080/openapi.yaml`
- Health check: `http://localhost:8080/healthz`

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

3. Create or update the local emulator user:

```bash
go run ./cmd/auth-emulator upsert-user \
  --uid 72sjYQv3GoNts3glUeiXmvbuBUx1 \
  --email testuser@example.com \
  --password 'Test123!'
```

4. Get an ID token from the emulator:

```bash
go run ./cmd/auth-emulator issue-token \
  --email testuser@example.com \
  --password 'Test123!'
```

5. Use the returned token to call a protected route:

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

## OpenAPI workflow

- Edit source files under `docs/spec/src`
- Bundle source files into `docs/spec/openapi.yaml` with `npm run openapi:bundle`
- Regenerate bundled spec and Go bindings with `npm run openapi:generate`

### Tooling

Install the Node dependency once:

```bash
npm install
```

Install `oapi-codegen` once:

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
```

The generated Go bindings live in `internal/http/api/openapi.gen.go`.

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

## Legacy migration planning

- Validate the checked-in legacy JSON exports and generate the Task 5 architecture report: `go run ./cmd/migrate_legacy plan`
- Override source/report directories when needed: `go run ./cmd/migrate_legacy plan --source-dir docs/mirgations --report-dir artifacts/legacy_migration`

## Project structure

```text
cmd/api                 Application entrypoint
docs/spec               OpenAPI and schema specs
docker/postgres/init    Local database bootstrap scripts
internal/application    Application services
internal/config         Environment config loading
internal/domain         Domain models and business rules
internal/http           HTTP handlers and router
internal/platform       Infrastructure adapters
internal/server         HTTP server bootstrap
internal/shared         Shared cross-cutting utilities
```
