# CI/CD Strategy

> Status: historical/provisional delivery notes. Current executable pipeline
> contracts are `.github/workflows/**`, `cloudbuild.staging.yaml`, and
> `docs/staging-runbook.md`; this file is not production readiness evidence.

## Goal

The goal of this CI/CD plan is to provide the minimum necessary delivery guardrails for the backend without introducing unnecessary platform complexity.

The backend has meaningful database schema, OpenAPI contracts, cross-module business flows, scheduler jobs, and legacy data migration concerns. A useful pipeline must therefore verify more than compilation, but it does not need Jenkins, Kubernetes-only deployment, canary releases, or a full enterprise release platform at this stage.

## Principles

- Keep PR checks fast enough to run on every change.
- Use a real test environment for deployed API verification.
- Treat core E2E flows as release acceptance before the first launch.
- Do not require exhaustive E2E coverage for every edge case before first launch.
- Keep production smoke tests small, low-risk, and reversible.
- Prefer GitHub Actions and/or GCP Cloud Build over self-managed CI infrastructure.
- Add tests as regressions are found instead of trying to predict every case upfront.

## Pipeline Layers

### 1. PR / Commit CI

Purpose: catch local regressions before code is merged.

Recommended checks:

- Run `go test ./...`.
- Run a build check.
- Verify OpenAPI generated code is in sync with the spec.
- Run a migration smoke test against a temporary PostgreSQL database.
- Run the opt-in E2E harness against a dedicated PostgreSQL database and Firebase Auth Emulator.
- Run formatting or lint checks if the project enables them.

This layer should be fast, stable, and suitable for every PR.

### 2. Dev / Test Environment Validation

Purpose: verify that the backend works after deployment with real service wiring, middleware, database access, and controlled test data.

Recommended flow:

1. Build the application image.
2. Push the image to Artifact Registry.
3. Deploy to the dev/test environment.
4. Run migrations against the dev/test Cloud SQL database.
5. Seed controlled test data.
6. Run core API E2E flows.
7. Run representative scheduler job checks.
8. Run OpenAPI contract checks against the deployed API where practical.

This layer can be slower than PR CI, but it must be repeatable and automated enough to be trusted.

### 3. Production Deployment

Purpose: update production safely and quickly verify that the deployed service is usable.

Recommended flow:

1. Confirm PR CI and dev/test validation passed.
2. Confirm production database backup or point-in-time recovery is available.
3. Run backward-compatible migrations.
4. Deploy the new application version.
5. Run production smoke tests.
6. If smoke tests fail, roll back the application version first.
7. Avoid emergency database rollback unless there is a tested runbook.

Production smoke tests should not be full E2E tests. They should be small checks that confirm the deployed service, configuration, database connection, and key read paths are healthy.

Recommended production smoke checks:

- Health endpoint.
- Readiness or database connectivity endpoint if available.
- Migration version check if available.
- Authentication middleware smoke check.
- One or more read-only API checks using a smoke account.
- Log check for startup panics, migration errors, or repeated request failures.
- Optional controlled write/delete smoke only if data isolation and cleanup are guaranteed.

## Migration Smoke Test

A migration smoke test starts a temporary PostgreSQL database in CI and runs all migrations from an empty schema.

It verifies:

- Migration SQL syntax.
- Migration ordering.
- Table, index, foreign key, and check constraint creation.
- The migration runner can connect to PostgreSQL and complete successfully.
- A clean environment can build the current schema.

It does not replace real environment migrations. Dev, staging, and production Cloud SQL databases still need their own migrations during deployment.

## Core E2E Scope

Core E2E tests are required before the first launch because they act as end-to-end acceptance for the current backend. They do not need to cover every extreme edge case, but they should cover the main business flows and the most important rejection paths.

The E2E harness runs only through an explicit command:

```bash
go test -tags=e2e ./test/e2e
```

It requires an already-running API, a dedicated non-production PostgreSQL database, the Firebase Auth Emulator, and test account credentials. The harness resets the dedicated database schema, runs migrations, seeds controlled backend user data, obtains a real Firebase emulator ID token, and then calls the API over HTTP. The database name must contain `e2e` or `test` before reset is allowed.

Recommended first-launch E2E coverage:

- Authentication and authorization scope.
- Property and room lifecycle.
- Tenant and lease creation.
- Lease-created side effects such as occupied room state and generated bills.
- Billing meter recording.
- Bill payment and accounting/report read paths.
- Lease rent adjustment or replacement, depending on launch scope.
- Normal lease termination and unpaid-bill rejection.
- Force termination and bill write-off behavior.
- Repair maintenance flow if included in launch scope.
- Scheduler job smoke or representative batch flows.
- Attachment upload-token and registration flow if included in launch scope.
- Legacy migrated data read checks.

The first-launch suite should prove that the backend can support the main operational workflows. Extended edge-case E2E tests can be added incrementally after launch.

## Legacy Data Migration

The first launch has an additional risk: existing legacy data must be migrated into the new schema. This should be treated as a release prerequisite, not as a normal CI unit test.

Before launch, validate legacy migration in a staging-like environment:

- Run clean schema migrations.
- Run legacy data import.
- Produce a migration validation report.
- Reconcile source and target counts.
- Review failed and skipped rows.
- Run API read checks against migrated data.
- Run core E2E flows on top of migrated or representative migrated data.

Production cutover should have:

- A legacy data freeze or delta migration strategy.
- A migration runbook.
- Backup and restore plan.
- Abort criteria.
- Post-migration validation checklist.
- Production smoke checklist.
- Manual sign-off point.

## Minimal First Implementation

Before first launch, the minimum useful CI/CD setup should include:

- PR CI with `go test ./...`.
- Build check.
- OpenAPI generated-code sync check.
- Migration smoke test with temporary PostgreSQL.
- Dev/test deployment.
- Migrations against dev/test Cloud SQL.
- Legacy migration dry-run or staging run.
- Core API E2E acceptance suite.
- Post-migration validation report.
- Production deployment runbook.
- Production smoke checklist.

The following can be added later:

- Exhaustive E2E edge-case coverage.
- Fully automated production deployment.
- Advanced rollback automation.
- Load testing.
- Canary or blue-green release strategy.
- Durable outbox or queue infrastructure.

## Tooling Recommendation

Recommended stack:

- GitHub Actions for PR CI, or Cloud Build PR triggers if CI should stay inside GCP.
- GCP Cloud Build for build and deploy when targeting GCP.
- Artifact Registry for container images.
- Cloud Run or the chosen GCP runtime.
- Cloud SQL PostgreSQL.
- Secret Manager.
- Cloud Logging and Error Reporting.

Jenkins is not required for the current project scope.

## Non-Goals

- Do not introduce Jenkins only for CI/CD orchestration.
- Do not introduce Kubernetes unless runtime requirements justify it.
- Do not introduce complex blue-green or canary release before basic validation is stable.
- Do not block first launch on exhaustive E2E coverage for every edge case.
- Do not use production smoke tests as a replacement for dev/test validation.
