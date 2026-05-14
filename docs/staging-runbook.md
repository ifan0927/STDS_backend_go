# Staging Deployment Runbook

Status: staging v1 contract for the first demo wave.

This runbook defines the repository-side staging contract. The human operator
performs GCP Console or `gcloud` changes. Do not paste secret values, tokens,
private keys, database passwords, or full bearer credentials into issues, pull
requests, logs, or evidence.

## Scope

Staging v1 includes:

- Firebase Hosting admin frontend with `/api/**` rewrite to the core backend.
- Cloud Run core backend.
- Cloud SQL PostgreSQL using private IP.
- VPC / Direct VPC egress from Cloud Run to Cloud SQL.
- Secret Manager environment injection.
- Cloud Storage signed URL attachment upload.
- Firebase Auth integration.
- Resend email configuration.
- Cloud Logging / Monitoring baseline.

Staging v1 excludes:

- Cloud Scheduler jobs and scheduler smoke checks.
- Brand thin backend deployment.
- Production deployment gates.
- Preview or candidate environments.

Scheduler endpoints remain protected by `APP_SCHEDULER_KEY` when enabled in a
later pass. Do not create Cloud Scheduler jobs for the first staging demo wave.

## Runtime Environment

Set these plain Cloud Run environment variables for the core backend:

```text
APP_NAME=stds-backend
APP_ENV=staging
APP_HOST=0.0.0.0
APP_PORT=8080
GIN_MODE=release
APP_BASE_URL=https://<staging-admin-domain>
APP_READ_TIMEOUT=5s
APP_WRITE_TIMEOUT=10s
APP_SCHEDULER_JOB_TIMEOUT=2m
APP_SCHEDULER_MAX_RETRIES=3
FIREBASE_PROJECT_ID=<staging-firebase-project-id>
FIREBASE_PASSWORD_RESET_REDIRECT_URL=https://<staging-admin-domain>/reset-password
RESEND_FROM_EMAIL=<verified-staging-sender>
RESEND_FROM_NAME=STDS
RESEND_BASE_URL=https://api.resend.com
GCS_BUCKET_NAME=<staging-attachments-bucket>
GCS_SIGNED_URL_TTL=15m
DB_MAX_OPEN_CONNS=5
DB_MAX_IDLE_CONNS=2
DB_CONN_MAX_LIFETIME=5m
```

Inject these values from Secret Manager:

```text
DATABASE_URL
RESEND_API_KEY
```

Do not set these local/test-only variables in staging:

```text
FIREBASE_AUTH_EMULATOR_HOST
FIREBASE_TEST_UID
STORAGE_EMULATOR_HOST
ATTACHMENT_STORAGE_MODE=fake-metadata
GOOGLE_APPLICATION_CREDENTIALS
```

Cloud Run should use the runtime service account and Application Default
Credentials instead of a checked-in or mounted service account key file.

## Container Image

Build the backend API image from the repository root:

```bash
docker build -t stds-backend:staging .
```

Run a local smoke container against the local PostgreSQL container from Docker
Desktop on macOS:

```bash
docker compose up -d postgres
docker run --rm --name stds-backend-smoke \
  -p 8080:8080 \
  -e APP_ENV=e2e \
  -e APP_PORT=8080 \
  -e GIN_MODE=release \
  -e DATABASE_URL='postgres://stds:stds@host.docker.internal:5432/stds_backend?sslmode=disable' \
  -e FIREBASE_PROJECT_ID=demo-stds-backend \
  -e FIREBASE_AUTH_EMULATOR_HOST=host.docker.internal:9099 \
  -e RESEND_API_KEY=local-smoke-only \
  -e RESEND_FROM_EMAIL=dev@example.com \
  -e GCS_BUCKET_NAME=stds-local-smoke \
  -e ATTACHMENT_STORAGE_MODE=fake-metadata \
  stds-backend:staging
```

From another terminal, verify health:

```bash
curl http://localhost:8080/healthz
```

The expected smoke result is HTTP 200 with `"ok":true`. `"db":true` requires
the local database to be reachable and migrated. Stop the smoke container with
`docker stop stds-backend-smoke`.

The runtime image contains only the compiled `cmd/api` binary plus Alpine
runtime packages for CA certificates and timezone data. Do not copy `.env`
files, credentials, private keys, or other local secrets into the image.

## Dev To Staging Deployment

The first staging deployment wave uses a manual GitHub Actions trigger. Do not
enable automatic `push` deployment from the `staging` branch until the first GCP
setup, deploy, and smoke evidence have been reviewed.

The intended flow is:

1. Merge application changes to `dev` through the existing PR CI gate.
2. Choose the exact `dev` commit SHA or staging branch ref to deploy.
3. Manually run the `Staging Deploy` GitHub Actions workflow with that ref and
   an image tag.
4. Let GitHub Actions run staging predeploy checks.
5. Let Cloud Build build the image, push Artifact Registry, and deploy Cloud Run.
6. Run minimal smoke from the workflow only when requested, then complete the
   fuller deployed smoke checklist under the staging smoke issue.

The predeploy checks should catch low-cost failures before touching GCP:

- Go tests and API build.
- OpenAPI lint and generated artifact sync.
- Empty PostgreSQL migration smoke.
- Docker image build.
- Staging deployment contract checks for manual trigger, Cloud Build config, and
  Secret Manager references.

GitHub Actions is the trigger and status-reporting entry point. Deployment-layer
execution stays in Cloud Build. Do not put secret values, database passwords,
private keys, or full bearer credentials into GitHub workflow files, Cloud Build
substitutions, logs, pull requests, or issue evidence.

### GitHub Configuration

Configure these GitHub repository variables before running the manual workflow:

```text
STAGING_GCP_PROJECT_ID
STAGING_REGION
STAGING_WORKLOAD_IDENTITY_PROVIDER
STAGING_GITHUB_DEPLOY_SERVICE_ACCOUNT
STAGING_CLOUD_BUILD_SERVICE_ACCOUNT_EMAIL
STAGING_CLOUD_RUN_SERVICE
STAGING_RUNTIME_SERVICE_ACCOUNT_EMAIL
STAGING_VPC_NETWORK
STAGING_VPC_SUBNET
STAGING_VPC_EGRESS
STAGING_ARTIFACT_REGISTRY_REPO
STAGING_APP_BASE_URL
STAGING_SMOKE_BASE_URL
STAGING_FIREBASE_PROJECT_ID
STAGING_RESEND_FROM_EMAIL
STAGING_GCS_BUCKET_NAME
STAGING_DATABASE_URL_SECRET
STAGING_RESEND_API_KEY_SECRET
```

These values are configuration identifiers, service account emails, resource
names, or public staging URLs. Secret values remain in Secret Manager and must
not be copied into GitHub repository variables.

`STAGING_APP_BASE_URL` is the browser-facing application URL used by the
backend for generated links. `STAGING_SMOKE_BASE_URL` is the deployed backend
base URL used by the optional minimal smoke step and must route directly to the
backend paths `/healthz` and `/openapi.yaml`.

`STAGING_GITHUB_DEPLOY_SERVICE_ACCOUNT` is the account GitHub OIDC impersonates
to submit the build. `STAGING_CLOUD_BUILD_SERVICE_ACCOUNT_EMAIL` is the Cloud
Build execution account email; the workflow expands it into the Cloud Build
resource path expected by `gcloud builds submit`. That account needs Artifact
Registry push, Cloud Run deploy, runtime service-account act-as, and any
explicitly approved migration connectivity for later phases.

`STAGING_RUNTIME_SERVICE_ACCOUNT_EMAIL` is the Cloud Run runtime identity. It
must be a full service account email, not a bare account id. `STAGING_VPC_NETWORK`
and `STAGING_VPC_SUBNET` configure Cloud Run Direct VPC egress for private Cloud
SQL connectivity. `STAGING_VPC_EGRESS` should normally be `private-ranges-only`
for staging v1 unless operator evidence justifies a different value.

### Cloud Build Configuration

`cloudbuild.staging.yaml` accepts these substitutions:

```text
_REGION
_SERVICE_NAME
_ARTIFACT_REGISTRY_REPO
_IMAGE_NAME
_IMAGE_TAG
_RUNTIME_SERVICE_ACCOUNT
_VPC_NETWORK
_VPC_SUBNET
_VPC_EGRESS
_APP_BASE_URL
_FIREBASE_PROJECT_ID
_RESEND_FROM_EMAIL
_GCS_BUCKET_NAME
_DATABASE_URL_SECRET
_RESEND_API_KEY_SECRET
```

Cloud Build performs:

1. Docker image build from the repository `Dockerfile`.
2. Artifact Registry push.
3. Cloud Run deploy for the core backend service with Direct VPC egress.

`DATABASE_URL` and `RESEND_API_KEY` are injected into Cloud Run from Secret
Manager by secret name. The staging workflow and Cloud Build config must not
contain the secret values.

### Deployment Evidence

Attach only non-secret evidence after a staging deploy:

- GitHub Actions run URL and selected source ref.
- Cloud Build build ID and final status.
- Artifact Registry image path and tag.
- Cloud Run service name, region, revision, runtime service account, ingress,
  min/max instances, and redacted env summary.
- Secret Manager secret names referenced by Cloud Run, without values.
- Minimal smoke status when `run_smoke` was enabled.

If deployment fails, keep Cloud Build and Cloud Run logs available for diagnosis
and rerun from a known `dev` ref after fixing the repo-side or GCP-side cause.
Do not assume automatic database rollback.

## Secret Manager

Recommended staging secret names:

```text
stds-staging-database-url
stds-staging-resend-api-key
```

Later scheduler enablement should add:

```text
stds-staging-scheduler-key
```

The runbook and review evidence should list secret names and IAM bindings only.
Never include secret values.

## Service Accounts And IAM

Create service-specific accounts instead of using broad default service
accounts.

Core backend runtime service account:

```text
stds-cloud-run-core-staging
```

Expected access:

- Secret Manager Secret Accessor for core backend staging secrets only.
- Cloud SQL Client for the staging Cloud SQL instance.
- Object access needed for the staging attachment bucket signed URL and
  metadata flows.
- No project-wide Owner or Editor role.

Cloud Build service account:

```text
stds-cloud-build-staging
```

Expected access:

- Push to the staging Artifact Registry repository.
- Deploy or update the staging Cloud Run service.
- Act as the core backend runtime service account only for deployment.
- Access private Cloud SQL for migrations only after the migration path is
  explicitly documented and verified.

Cloud Scheduler is intentionally excluded from staging v1. Do not create a
scheduler service account or scheduler jobs for the first demo wave.

## Cloud Run

Initial staging settings:

```text
region=asia-east1
min-instances=0
max-instances=2
DB_MAX_OPEN_CONNS=5
DB_MAX_IDLE_CONNS=2
DB_CONN_MAX_LIFETIME=5m
```

Keep Cloud Run container concurrency at the platform default unless staging
evidence shows a reason to change it.

The browser-facing API contract is Firebase Hosting `/api/**`, not the Cloud Run
`run.app` URL. Do not assume the default `run.app` URL can be disabled until it
is verified with the selected Hosting rewrite path. If the default URL remains
reachable, Firebase Auth, DB-backed authorization, and application-level checks
remain the security boundary.

## Cloud SQL And VPC

Staging v1 requires:

- Cloud SQL PostgreSQL staging database.
- Staging-specific database credential.
- Private IP enabled.
- VPC and subnet configured for Cloud Run Direct VPC egress.
- No public DB IP as the default access path.

The operator must document how migrations reach private Cloud SQL. If Cloud
Build cannot reach the private database in the first pass, migrations must be a
manual operator step with non-secret evidence. Do not temporarily enable public
DB IP unless an explicit exception, rollback plan, and expiry are documented.

Operators also need a documented database inspection path that does not require
enabling public IP.

## Staging Database Preparation

Prepare the staging demo database as one coordinated operator step after the
repo-side contracts for the staging issue set have landed. The preparation path
uses the documented private Cloud SQL access path and must not temporarily
enable a public database IP unless an explicit exception, rollback plan, and
expiry are documented.

The operator runner can be a Cloud Build private pool, a controlled VM, or
another approved environment. It must have:

- a checkout of this repository.
- the project Go toolchain.
- `psql`.
- private network access to the staging Cloud SQL database.
- `DATABASE_URL` provided without printing the value.

Run the steps in this order against the staging database:

1. Apply schema migrations.
2. Import legacy data from the checked-in legacy JSON exports.
3. Validate the imported legacy data.
4. Bootstrap the first staging admin user by aligning a Firebase Auth user with
   a backend `users` row.
5. Run deployed smoke checks through the staging API.

The staging database must not be destructively reset as part of this runbook.
If a preparation step fails, stop the deployment or demo handoff, keep the
failed state for diagnosis where practical, and attach non-secret evidence.
Do not assume automatic `down` rollback for staging.

### Schema Migrations

Run pending schema migrations with the same embedded migration runner used by
local operations and PR CI:

```bash
go run ./cmd/migrate up
```

`DATABASE_URL` must target the staging Cloud SQL database through the documented
private access path. Do not print the connection string. If this command runs in
Cloud Build later, the build service account needs only the secret and private
Cloud SQL access required for this step. If Cloud Build private connectivity is
not ready in the first pass, run this as a manual operator step and attach
non-secret evidence.

Expected evidence:

- command status or Cloud Build step ID.
- latest `schema_migrations.version`.
- redacted database target summary showing staging database name only.

Operator script from the repository root:

```bash
#!/usr/bin/env bash
set -euo pipefail
set +x

: "${DATABASE_URL:?DATABASE_URL must point to the staging database}"

go run ./cmd/migrate up

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -c \
  "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;"
```

### Legacy Data Import

Legacy data import is separate from schema migration. Confirm the source
inventory, then use the existing stage-owned legacy migration commands against
the same staging database:

```bash
go run ./cmd/migrate_legacy plan
go run ./cmd/migrate_legacy properties
go run ./cmd/migrate_legacy rooms
go run ./cmd/migrate_legacy tenants
go run ./cmd/migrate_legacy leases
go run ./cmd/migrate_legacy room-status
go run ./cmd/migrate_legacy bills
go run ./cmd/migrate_legacy journal
go run ./cmd/migrate_legacy validate
```

The source for this staging demo wave is the checked-in legacy JSON export set
under `docs/mirgations`. Do not read directly from the legacy production
database during staging setup. The command writes reports under
`artifacts/legacy_migration`; review the validation report before treating the
database as demo-ready.

Expected evidence:

- stage command status for each legacy import step.
- `task13_validation_report.json` summary counts.
- confirmation that validation completed without required-check failures.
- mapping-table counts for `legacy_property_mappings`,
  `legacy_room_mappings`, `legacy_tenant_mappings`, `legacy_lease_mappings`,
  `legacy_bill_mappings`, and `legacy_schedule_mappings`.

Operator script from the repository root:

```bash
#!/usr/bin/env bash
set -euo pipefail
set +x

: "${DATABASE_URL:?DATABASE_URL must point to the staging database}"

go run ./cmd/migrate_legacy plan
go run ./cmd/migrate_legacy properties
go run ./cmd/migrate_legacy rooms
go run ./cmd/migrate_legacy tenants
go run ./cmd/migrate_legacy leases
go run ./cmd/migrate_legacy room-status
go run ./cmd/migrate_legacy bills
go run ./cmd/migrate_legacy journal
go run ./cmd/migrate_legacy validate

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'legacy_property_mappings' AS table_name, COUNT(*) FROM legacy_property_mappings
UNION ALL
SELECT 'legacy_room_mappings', COUNT(*) FROM legacy_room_mappings
UNION ALL
SELECT 'legacy_tenant_mappings', COUNT(*) FROM legacy_tenant_mappings
UNION ALL
SELECT 'legacy_lease_mappings', COUNT(*) FROM legacy_lease_mappings
UNION ALL
SELECT 'legacy_bill_mappings', COUNT(*) FROM legacy_bill_mappings
UNION ALL
SELECT 'legacy_schedule_mappings', COUNT(*) FROM legacy_schedule_mappings
ORDER BY table_name;
SQL
```

### First Staging Admin Bootstrap

The first staging admin has a chicken-and-egg boundary: normal `POST /users`
requires an existing authenticated admin, but the first admin does not exist
yet. For staging v1, bootstrap exactly one initial admin through an operator
step, then create later users through the normal admin API.

1. Create or identify the first admin user in the staging Firebase Auth project.
2. Record the Firebase UID and email as non-secret setup inputs.
3. Upsert the matching backend user row through the documented private database
   access path:

Operator script from the repository root or another controlled runner with
private staging database access:

```bash
#!/usr/bin/env bash
set -euo pipefail
set +x

: "${DATABASE_URL:?DATABASE_URL must point to the staging database}"
: "${STAGING_ADMIN_FIREBASE_UID:?first admin Firebase UID is required}"
: "${STAGING_ADMIN_EMAIL:?first admin email is required}"
: "${STAGING_ADMIN_NAME:?first admin display name is required}"

psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v firebase_uid="$STAGING_ADMIN_FIREBASE_UID" \
  -v admin_email="$STAGING_ADMIN_EMAIL" \
  -v admin_name="$STAGING_ADMIN_NAME" <<'SQL'
INSERT INTO users (
    firebase_uid,
    email,
    name,
    role,
    permission_overrides,
    assigned_property_ids
) VALUES (
    :'firebase_uid',
    :'admin_email',
    :'admin_name',
    'admin',
    '[]'::jsonb,
    '[]'::jsonb
)
ON CONFLICT (firebase_uid) WHERE deleted_at IS NULL
DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    role = EXCLUDED.role,
    permission_overrides = EXCLUDED.permission_overrides,
    assigned_property_ids = EXCLUDED.assigned_property_ids,
    updated_at = now(),
    version = users.version + 1
RETURNING id, firebase_uid, email, role, deleted_at IS NULL AS active;
SQL
```

The SQL executed by the script is:

```sql
INSERT INTO users (
    firebase_uid,
    email,
    name,
    role,
    permission_overrides,
    assigned_property_ids
) VALUES (
    '<firebase_uid>',
    '<admin_email>',
    '<admin_name>',
    'admin',
    '[]'::jsonb,
    '[]'::jsonb
)
ON CONFLICT (firebase_uid) WHERE deleted_at IS NULL
DO UPDATE SET
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    role = EXCLUDED.role,
    permission_overrides = EXCLUDED.permission_overrides,
    assigned_property_ids = EXCLUDED.assigned_property_ids,
    updated_at = now(),
    version = users.version + 1
RETURNING id, firebase_uid, email, role, deleted_at IS NULL AS active;
```

After the first admin can sign in, run `POST /auth/sync` through the staging
frontend/API path and use the normal user-management APIs for additional demo
users. Do not use the local Firebase Auth Emulator bootstrap commands in
staging. Do not give Cloud Build or the database preparation runner Firebase
Auth admin permissions for this first staging wave; Firebase Auth user creation
remains an operator action in the Firebase project.

Expected evidence:

- Firebase Auth project ID.
- first admin Firebase UID and email.
- backend `users` row ID, role, and `deleted_at IS NULL` status.
- successful `/auth/sync` result for the first admin, with bearer token
  redacted.

## Cloud Storage

The staging attachment bucket must not be public. Browser upload uses scoped
signed URLs from the backend.

Bucket CORS must allow only approved staging frontend origins. At minimum it
must support:

```text
method=PUT
header=Content-Type
```

The core backend runtime service account needs enough bucket access to generate
signed upload/download URLs and read object metadata during attachment
registration checks.

## Logging, Monitoring, And Alerting

Staging uses GCP-native Cloud Logging and Cloud Monitoring. Do not add
Prometheus, Grafana, custom metrics, tracing, or APM for the first demo wave
unless a concrete staging blocker requires it.

### Runtime Logging Contract

Cloud Run automatically sends these log streams to Cloud Logging:

- Cloud Run request logs.
- Container stdout and stderr.
- Cloud Run system and revision logs.

The backend writes JSON structured logs to stdout through Go `log/slog`.
Non-local environments use info level by default. Each application log includes
the baseline `service` and `env` fields.

HTTP access logs are emitted by the Gin logging middleware after each request.
Expected fields include:

```text
request_id
method
path
status
latency_ms
user_id
firebase_uid
role
property_id
error_code
job_key
job_window_key
job_status
job_retry_count
job_duration_ms
```

`property_id` is populated when the route resolver can determine the authorized
property. Scheduler fields are populated only for scheduler-trigger endpoints.

HTTP 5xx errors add a structured error log with `request_id`, `method`, `path`,
`error_code`, and `cause`. Panic recovery adds `request_id`, `method`, `path`,
`panic`, and `stack`.

The migration commands used during database preparation write plain stdout and
stderr output, not application JSON logs. Treat migration evidence as command
status, migration version, import counts, validation summaries, and redacted
database target metadata.

Application logs must remain allowlist-based. Do not log raw request objects,
raw request or response bodies, `Authorization` headers, Firebase ID tokens,
scheduler keys, DB passwords, private keys, Secret Manager values, full
`DATABASE_URL`, signed URL query strings, or credential file contents.
Treat Cloud Logging viewer access as sensitive operational access because logs
can include user, Firebase UID, role, property, request, and error context.

### Cloud Logging Queries

Use the Cloud Logging Logs Explorer. Start with the staging Cloud Run service
resource and narrow from there.

Replace placeholders before use:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="<staging-cloud-run-service>"
resource.labels.location="<staging-region>"
```

Recent application errors:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="<staging-cloud-run-service>"
resource.labels.location="<staging-region>"
severity>=ERROR
```

HTTP 5xx request logs:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="<staging-cloud-run-service>"
resource.labels.location="<staging-region>"
httpRequest.status>=500
```

Follow one request across access and error logs:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="<staging-cloud-run-service>"
resource.labels.location="<staging-region>"
jsonPayload.request_id="<request-id>"
```

Check one deployed path:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="<staging-cloud-run-service>"
resource.labels.location="<staging-region>"
jsonPayload.path="/healthz"
```

Authenticated smoke requests should prove that `user_id`, `firebase_uid`, and
`role` are present when expected, but evidence must never include the bearer
token used to make the request.

Attachment smoke should prove the upload/download endpoints, status, and
`request_id` are visible. Do not attach full signed upload/download URLs,
nonces, or signed URL query strings as evidence.

Scheduler endpoint logs are not required for staging v1 because Cloud Scheduler
jobs are excluded from the first demo wave. When scheduler jobs are enabled
later, inspect `jsonPayload.job_key`, `jsonPayload.job_window_key`,
`jsonPayload.job_status`, `jsonPayload.job_retry_count`, and
`jsonPayload.job_duration_ms`.

Current scheduler logging has an important limit: job fields are attached to
the HTTP access log after a scheduler trigger returns a result. If the trigger
fails before returning that result, the request may only have normal
`error_code` and `cause` fields in the error log. Use the persisted
`scheduler_job_runs` row for `job_key`, `window_key`, `status`, and `message`
when investigating scheduler failures after scheduler jobs are enabled.

### Current Gaps To Account For

These are accepted staging v1 constraints, not blockers for the first demo
wave:

- Successful API startup does not emit a dedicated application startup log; use
  Cloud Run revision/system logs and `/healthz` smoke evidence to confirm the
  revision is serving.
- 5xx `cause`, panic values, stack traces, and migration driver errors are not
  redacted by a centralized scrubber. Evidence must be reviewed before posting.
- Scheduler failure logs may not include scheduler-specific fields when failure
  happens before result metadata is attached.
- Batch job summaries do not persist per-item failure details in structured log
  fields.
- `cmd/migrate` and `cmd/migrate_legacy` output is command-line text rather
  than JSON structured logging.

### Cloud Monitoring Checks

Inspect these Cloud Run metrics for the staging service after deploy and smoke:

- Request count.
- Request latency.
- 4xx and 5xx response count.
- Container instance count.
- CPU and memory utilization.

Inspect these Cloud SQL metrics for the staging database:

- CPU utilization.
- Storage utilization.
- Connection count.
- Memory pressure where available for the selected machine type.

Compare Cloud SQL connection count with the actual `max_connections` result
captured during database setup. The Cloud Run runtime env should keep
`DB_MAX_OPEN_CONNS=5`, `DB_MAX_IDLE_CONNS=2`, and
`DB_CONN_MAX_LIFETIME=5m` for staging v1 unless later evidence justifies a
change.

### Alerting Baseline

Configure alert policies only for actionable staging signals:

- Cloud Run service unavailable or uptime check failing.
- Elevated Cloud Run 5xx count or error ratio.
- Cloud Run instance count reaches the configured `max-instances`.
- Cloud SQL CPU sustained above the accepted staging threshold.
- Cloud SQL connection count approaching the verified `max_connections` limit.
- Cloud SQL storage utilization above the accepted staging threshold.
- Billing budget threshold for the staging project.

Use a human-operated notification channel such as email for staging. Do not add
pager-style production incident routing for this first demo wave.

Keep log-based metrics optional. If one is added, keep it narrow and low-volume,
such as scheduler failed results after scheduler jobs are introduced, auth
validation failures, or signed URL generation count. Do not add request or
response body logging to support metrics.

### Observability Evidence

Attach only non-secret evidence:

- Cloud Run Logs Explorer filter for the staging service.
- One redacted Cloud Run request log showing method, path, status, and latency.
- One redacted application access log showing `request_id`, `service`, `env`,
  `path`, `status`, and `latency_ms`.
- One authenticated smoke access log showing that identity fields are present,
  with bearer tokens and unrelated user data omitted.
- One attachment smoke access log showing endpoint, status, and `request_id`,
  with signed URLs and URL query strings omitted.
- One redacted 5xx or synthetic error evidence if safely available; otherwise
  note that no 5xx occurred during smoke.
- Cloud Monitoring screenshots or metric names for Cloud Run request count,
  latency, 5xx count, and instance count.
- Cloud SQL metric names or screenshots for CPU, storage, and connection count.
- Alert policy names, conditions, thresholds, and notification channel metadata.
- Billing budget name and threshold summary.
- Confirmation that reviewed logs did not contain tokens, scheduler keys,
  database passwords, private keys, secret values, full `DATABASE_URL`, signed
  URL query strings, or full request/response bodies.
- Confirmation that external-provider errors, if any, were summarized without
  raw Firebase, Resend, GCS, or database error payloads.

## Verification Evidence

Attach only non-secret evidence to the issue or pull request.

Cloud Run evidence:

- Service name.
- Region.
- Latest revision ID.
- Runtime service account.
- Ingress setting.
- Min and max instances.
- Configured environment variable names with secret values redacted.

Secret Manager evidence:

- Secret names.
- IAM binding summary.
- Confirmation that no values were printed.

Cloud SQL / VPC evidence:

- Instance name.
- Database name.
- Private IP status.
- VPC / subnet / Direct VPC egress summary.
- Database user names only, without passwords.
- `max_connections` query result.
- Schema migration result and latest `schema_migrations.version`.
- Legacy migration validation summary.
- First admin Firebase UID/email and backend user row summary.
- Operator database inspection path.

Firebase Hosting evidence:

- Staging domain.
- `/api/**` rewrite summary.
- Browser smoke result through the Hosting URL.

Firebase Auth evidence:

- Protected API rejects missing or invalid bearer token.
- Protected API accepts a valid staging Firebase ID token for a seeded staging
  user.

GCS evidence:

- Bucket name.
- Public access prevention status.
- CORS summary.
- IAM binding summary.
- Signed URL upload, registration, and download smoke result.

Logging evidence:

- Cloud Run request log is visible for the staging service.
- Application structured JSON log is visible with `service`, `env`,
  `request_id`, `path`, `status`, and `latency_ms`.
- Cloud Logging filters used for request logs, application logs, and errors.
- Cloud Monitoring metric checks for Cloud Run request count, latency, 5xx
  count, instance count, and Cloud SQL CPU/storage/connections.
- Alert policy metadata and billing budget summary.
- Logs do not contain bearer tokens, scheduler keys, database passwords, secret
  values, private keys, full `DATABASE_URL`, signed URL query strings, or full
  request/response bodies containing sensitive data.

Scheduler evidence is not required for staging v1 because scheduler jobs are
outside the first demo wave.
