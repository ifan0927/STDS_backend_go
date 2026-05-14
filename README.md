# STDS Backend

Backend service for the STDS property management system, built with Go, Gin,
PostgreSQL, Firebase Auth, and OpenAPI-generated HTTP contracts.

## Local development quickstart

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

For Firebase Auth Emulator setup, local demo data, E2E flows, attachment
storage, and other operational details, see
[docs/local-operations.md](docs/local-operations.md).

## Common local scripts / commands

```bash
scripts/dev_auth_user.sh
scripts/dev_auth_users.sh
scripts/dev_demo_seed.sh
scripts/dev_brand_readonly.sh
./scripts/e2e_api.sh
./scripts/e2e_test.sh
./scripts/legacy_e2e_api.sh
./scripts/legacy_e2e_test.sh
```

Common migration commands:

```bash
go run ./cmd/migrate up
go run ./cmd/migrate down
go run ./cmd/migrate_legacy plan
```

## Test commands summary

```bash
go test ./...
go test -tags=e2e ./test/e2e
./scripts/e2e_test.sh
./scripts/legacy_e2e_test.sh
```

The E2E scripts expect local PostgreSQL and Firebase Auth Emulator dependencies.
See [docs/local-operations.md](docs/local-operations.md) for setup details.

## OpenAPI workflow

- Edit source files under `docs/spec/src`
- Bundle source files into `docs/spec/openapi.yaml` with `npm run openapi:bundle`
- Regenerate bundled spec and Go bindings with `npm run openapi:generate`

Install the Node dependency once:

```bash
npm install
```

Install `oapi-codegen` once:

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
```

The generated Go bindings live in `internal/http/api/openapi.gen.go`.

## Documentation index

- [Local operations](docs/local-operations.md): emulator setup, local scripts,
  E2E flows, environment variables, CI command details, and migration planning.
- [Cloud architecture](docs/cloud-architecture.md): proposed GCP staging and
  production-candidate architecture baseline.
- [Infrastructure guideline](docs/infra-guideline.md): infrastructure operating
  guidance.
- [Vertical slice handbook](docs/vertical-slice-handbook.md): implementation
  workflow and slice guidance.

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
