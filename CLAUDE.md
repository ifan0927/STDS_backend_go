# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Communication

All conversations with the user are in **Traditional Chinese**. All code comments are in **English**.

## Commands

```bash
# Run the API server
go run ./cmd/api

# Run all tests
go test ./...

# Run a single test package
go test ./internal/http/router/...

# Run a single test
go test ./internal/http/router/... -run TestGetPropertyUsesFormalAPIWiring

# Database migrations
go run ./cmd/migrate up
go run ./cmd/migrate down

# OpenAPI: bundle source files into docs/spec/openapi.yaml
npm run openapi:bundle

# OpenAPI: regenerate bundled spec and Go bindings
npm run openapi:generate
```

## Architecture

### Request lifecycle

Every authenticated request passes through three sequential middleware layers, compiled once at startup in `internal/http/router/router.go`:

1. **Authenticate** – verifies the Firebase Bearer token via `platformfirebase.Authenticator`, then resolves the caller to a DB `users.User`. The resolved DB record (not Firebase custom claims) is the source of truth for role and assigned property IDs.
2. **RequireRoles** – checks the DB principal's role against the route's `allowedRoles` list.
3. **RequirePropertyAccess** – resolves a property ID from the request (either a route param or a DB ownership lookup), then verifies the principal has access. The resolver still runs for `admin` so UUID validation and resource-specific not-found mapping stay consistent; `admin` bypasses only the access comparison. `owner` access is verified against `properties.owner_id`; all other roles are checked against `users.assigned_property_ids`.

Route policies are declared as a plain slice of `routePolicy` structs in `routePolicies()`. Adding a new route means appending to that slice — no middleware wiring required elsewhere.

### Auth strategies

Three strategies are selected per-route in `routePolicies`:

| Strategy | Middleware | When to use |
|---|---|---|
| `firebase` | `Auth` | Normal authenticated endpoints — resolves full DB user principal |
| `firebase_token_only` | `FirebaseTokenOnly` | Pre-registration endpoints (e.g. `POST /auth/sync`) — no DB user required |
| `scheduler` | `RequireSchedulerKey` | Internal cron endpoints — verified via `X-Scheduler-Key` header |

### OpenAPI code generation

HTTP handler signatures are generated from `docs/spec/openapi.yaml` into `internal/http/api/openapi.gen.go`. The `handler.APIServer` struct must implement the generated `StrictServerInterface`. When adding or changing endpoints, update the spec first, regenerate, then implement.

### Application layer

Application services live in `internal/application/` and have no HTTP imports or concrete database adapter imports. Transactional use cases may depend on `internal/platform/database/txrunner.Runner` and pass `*sql.Tx` through application-owned repository ports, matching the current project pattern. A broader refactor would be required before replacing this with an application-owned transaction abstraction.

### Error handling

API-facing errors flow through `internal/shared/apperr`. Check `internal/shared/apperr/common.go` before creating a reusable cross-package application error; package-specific application errors may live in the owning application package. Domain-layer sentinel errors stay transport-neutral, usually with `errors.New`, and are mapped to `apperr` at the application or HTTP boundary. Use `.WithCause(err)` and `.WithDetails(map)` for context. The `middleware.ErrorHandler` translates these to JSON responses. API-facing error messages are written in English.

### Firebase emulator

For local development without Firebase credentials, set `FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9099` and `FIREBASE_PROJECT_ID=demo-stds-backend`. Use `go run ./cmd/auth-emulator` to create users and issue tokens.

## Coding guidelines (from AGENTS.md)

- No features beyond what was asked. No abstractions for single-use code.
- Touch only what you must. Don't "improve" adjacent code unless it was broken by your changes.
- Remove imports/variables/functions made unused by YOUR changes only.
- Surface assumptions and tradeoffs before implementing; ask if unclear.
