# STDS Core Backend

STDS Core Backend is the central API service for a GCP-native property
management platform. It owns the operational data model, API contracts, identity
integration, authorization, billing, tenancy, repairs, attachments, reporting,
legacy migration, and backend deployment contracts.

The backend is built as a Go monolith with explicit internal boundaries. It
uses Gin, PostgreSQL, Firebase Auth, OpenAPI-generated HTTP contracts, and
GCP-managed infrastructure for the staging deployment line.

## System Overview

STDS is split across focused repositories:

- [STDS Core Backend](https://github.com/ifan0927/STDS_backend_go): source of
  truth for operational workflows and backend contracts.
- [STDS Admin Frontend](https://github.com/ifan0927/stds_fronted): internal
  management console for property operations.
- [STDS Brand Frontend](https://github.com/ifan0927/stds_brand_frontend):
  public-facing brand site deployed as a static Cloudflare Pages site.

The core backend remains the authority for operational state. Public brand
content is exposed through dedicated public read-only endpoints that read the
approved brand views and stay separate from the authenticated admin brand
management endpoints.

## Design Approach

- OpenAPI-first HTTP contracts with generated Go bindings.
- PostgreSQL schema, migrations, and constraints as the persistence source of
  truth.
- Firebase Auth for identity verification; DB user records remain the runtime
  authorization source for roles, permission overrides, and property scope.
- Layered Go structure: HTTP transport, application services, domain logic,
  database adapters, and server composition are kept separate.
- Structured JSON logging with request IDs, error codes, user/property context,
  and Cloud Logging-friendly fields.
- Direct attachment upload through backend-issued GCS signed URLs plus backend
  metadata registration.
- Legacy migration tooling and validation kept separate from normal runtime
  request handling.

## Cloud Architecture

The staging architecture uses GCP-managed services:

- Firebase Hosting for the browser-facing admin frontend.
- Cloudflare Pages for the browser-facing brand frontend.
- Cloud Run for backend services.
- Cloud SQL for PostgreSQL with private connectivity.
- Secret Manager for runtime secrets.
- Cloud Storage for attachments.
- Cloud Build and Artifact Registry for deployment.
- Firebase Auth for identity.
- Cloud Logging and Cloud Monitoring for baseline observability.

The first staging line has been deployed and verified for the core backend and
admin frontend. Production deployment, stricter ingress hardening, core public
brand endpoints, and brand frontend deployment are separate follow-up work.

## Runtime Responsibilities

```text
cmd/api                 Application entrypoint
docs/spec               OpenAPI and schema specs
internal/application    Use-case orchestration and transactions
internal/domain         Domain models and business rules
internal/http           Gin handlers, router, middleware, OpenAPI adapter
internal/platform       Database and external infrastructure adapters
internal/server         Runtime dependency wiring
internal/shared         Shared errors and cross-cutting contracts
```

## API Contract

The API contract is maintained under `docs/spec/src` and bundled into
`docs/spec/openapi.yaml`. Generated Go bindings live in
`internal/http/api/openapi.gen.go`.

```bash
npm run openapi:bundle
npm run openapi:generate
```

Local API docs:

```text
http://localhost:8080/scalar
http://localhost:8080/openapi.yaml
```

## Local Development

Minimal local startup:

```bash
docker compose up -d postgres
go run ./cmd/migrate up
go run ./cmd/api
```

Health check:

```text
http://localhost:8080/health
```

For Firebase Auth Emulator setup, local demo users, seed data, attachment
storage modes, E2E flows, and migration operations, see
[docs/local-operations.md](docs/local-operations.md).

## Testing

Common checks:

```bash
go test ./...
./scripts/e2e_test.sh
./scripts/legacy_e2e_test.sh
```

The E2E scripts expect local PostgreSQL and Firebase Auth Emulator dependencies.
See [docs/local-operations.md](docs/local-operations.md) for setup details.

## Documentation Map

- [Cloud architecture](docs/cloud-architecture.md): GCP staging and
  production-candidate architecture decisions.
- [Staging runbook](docs/staging-runbook.md): staging v1 deployment, secrets,
  IAM, private networking, and evidence checklist.
- [Local operations](docs/local-operations.md): local environment, scripts,
  E2E flows, and migration operations.
- [Infrastructure guideline](docs/infra-guideline.md): infrastructure coding
  rules for config, logging, auth, DB, errors, and middleware.
- [Vertical slice handbook](docs/vertical-slice-handbook.md): guidance for
  adding API slices in the existing architecture.
- [Domain model](docs/design/domain-model.md): domain model and business-rule
  baseline.
- [GitHub Wiki operator handbook](https://github.com/ifan0927/STDS_backend_go/wiki/Staging-v1-Operator-Handbook-2026-05-15-v1):
  long-term staging operator notes, IAM matrix, OIDC/WIF notes, troubleshooting
  ledger, and observability playbook.

## Current Status

Core backend and admin frontend staging v1 are complete for the first demo / UAT
wave. The staging line has proven container build, Cloud Build deployment, Cloud
Run startup, Cloud SQL private connectivity, Firebase Hosting admin frontend
deploy, and deployed Playwright smoke coverage.

This is not a production readiness claim. Production deployment and hardening
remain separate work.
