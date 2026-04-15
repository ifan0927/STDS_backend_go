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

## Database migrations

- Run all pending migrations: `go run ./cmd/migrate up`
- Roll back applied migrations in reverse order: `go run ./cmd/migrate down`

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
