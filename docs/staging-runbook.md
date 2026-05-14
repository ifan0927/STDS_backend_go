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
- Migration result.
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

- Cloud Run request log is visible.
- Application structured log is visible.
- Logs do not contain bearer tokens, scheduler keys, database passwords, secret
  values, or full request/response bodies containing sensitive data.

Scheduler evidence is not required for staging v1 because scheduler jobs are
outside the first demo wave.
