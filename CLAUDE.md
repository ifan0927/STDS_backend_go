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
3. **RequirePropertyAccess** – resolves a property ID from the request (either a route param or a DB ownership lookup), then verifies the principal has access. `admin` bypasses this check; `owner` access is verified against `properties.owner_id`; all other roles are checked against `users.assigned_property_ids`.

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

Application services live in `internal/application/` and have no HTTP or database imports — they depend on repository interfaces. This makes them straightforward to unit-test with fake implementations (see `router_test.go` for the fake pattern used throughout tests).

### Error handling

All errors flow through `internal/shared/apperr`. Use `apperr.New(code, httpStatus, message)` for sentinel errors in `internal/shared/apperr/common.go`. Use `.WithCause(err)` and `.WithDetails(map)` for context. The `middleware.ErrorHandler` translates these to JSON responses.

### Firebase emulator

For local development without Firebase credentials, set `FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9099` and `FIREBASE_PROJECT_ID=demo-stds-backend`. Use `go run ./cmd/auth-emulator` to create users and issue tokens.

## Coding guidelines (from AGENTS.md)

- No features beyond what was asked. No abstractions for single-use code.
- Touch only what you must. Don't "improve" adjacent code unless it was broken by your changes.
- Remove imports/variables/functions made unused by YOUR changes only.
- Surface assumptions and tradeoffs before implementing; ask if unclear.
