# STDS Backend

Minimal backend scaffold for local development with Go, Gin, PostgreSQL, and Scalar API docs.

## Local development

1. Copy `.env.example` to `.env` if needed.
2. Start PostgreSQL:

```bash
docker compose up -d postgres
```

3. Run the API:

```bash
go run ./cmd/api
```

4. Open local docs:

- Scalar UI: `http://localhost:8080/scalar`
- OpenAPI file: `http://localhost:8080/openapi.yaml`
- Health check: `http://localhost:8080/healthz`

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
