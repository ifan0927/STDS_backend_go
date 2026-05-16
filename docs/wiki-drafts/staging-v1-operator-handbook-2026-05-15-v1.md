# Staging v1 Operator Handbook

Target: GitHub Wiki page

Source of truth for operator handbook; repo docs keep contracts/checklists.

This handbook captures the staging v1 setup as operated through issue #202. It
is meant to be pasted into the GitHub Wiki and kept as the non-secret operator
runbook for the current staging line.

Do not paste secret values, tokens, passwords, full `DATABASE_URL` values,
signed URLs, raw request/response payloads, or PII-heavy screenshots into this
page.

## Scope And Status

Staging v1 is complete for the core backend plus admin frontend first demo
wave:

- Core backend deploy path: GitHub Actions -> Cloud Build -> Artifact Registry
  -> Cloud Run is proven.
- Core backend runtime startup is proven against the private Cloud SQL path
  after the bounded startup DB ping retry fix.
- Admin frontend deploy path: GitHub Actions -> Cloud Build -> Firebase
  Hosting is proven.
- Admin frontend deployed Playwright smoke passed against Firebase Hosting.
- Brand frontend Cloudflare Pages deployment and core public brand endpoints are
  later work, not part of this v1 completion.

Cloud Run deploy success is not the same as full staging readiness. Database
import, auth seed state, GCS, Resend, logging, and frontend E2E evidence must be
checked separately when replaying the environment.

## Stable Identifiers

| Area | Value |
| --- | --- |
| GCP project | `stds-439609` |
| Primary region | `asia-east1` |
| Backend Cloud Run service | `stds-staging-core-backend` |
| Cloud SQL instance | `stds-staging-postgres` |
| Cloud SQL database | `stds_backend` |
| Cloud SQL private IP | `10.10.0.3` |
| VPC / subnet | `stds-staging-vpc` / `stds-staging-asia-east1` |
| Cloud Run VPC egress | `private-ranges-only` |
| Artifact Registry repo | `stds-staging-backend` |
| Backend image name | `stds-backend` |
| Cloud Build source bucket | `gs://stds-439609_cloudbuild` |
| Firebase Hosting URL | `https://stds-439609.web.app` |
| Backend DB secret name | `stds-staging-database-url` |
| Backend Resend secret name | `stds-staging-resend-api-key` |
| Frontend E2E password secret name | `frontend-staging-e2e-password` |
| Frontend E2E email | `staging-e2e-admin@stds-staging.local` |

## Service Accounts And IAM

| Identity | Purpose | Non-secret role summary |
| --- | --- | --- |
| Backend WIF principalSet for `ifan0927/STDS_backend_go` | GitHub Actions federation entry point | Can federate through the backend provider and impersonate the backend GitHub deploy service account. |
| `stds-github-deploy-staging@stds-439609.iam.gserviceaccount.com` | Backend GitHub deploy service account | Submits Cloud Build, uses the project Cloud Build source bucket, and can act as `stds-cloud-build-staging`. |
| `stds-cloud-build-staging@stds-439609.iam.gserviceaccount.com` | Backend Cloud Build execution service account | Builds and pushes the image, writes build logs, deploys Cloud Run, and can act as the backend runtime service account. Needs deploy-level Cloud Run permission; `roles/run.builder` alone was insufficient for this flow. |
| `stds-cloud-run-core-staging@stds-439609.iam.gserviceaccount.com` | Backend Cloud Run runtime service account | Reads backend staging secrets, connects to Cloud SQL, and accesses the staging attachment bucket for signed URL flows. |
| Frontend WIF principalSet for `ifan0927/stds_fronted` | Frontend GitHub Actions federation entry point | Limited to the frontend repo federation path. |
| `frontend-github-deploy-staging@stds-439609.iam.gserviceaccount.com` | Frontend GitHub deploy service account | Submits frontend Cloud Build, acts as the frontend Cloud Build service account, and reads the single E2E password secret when post-deploy E2E is enabled. |
| `frontend-cloud-build-staging@stds-439609.iam.gserviceaccount.com` | Frontend Cloud Build execution service account | Deploys Firebase Hosting only. It does not need backend DB, backend GCS attachment bucket, Resend, or production permissions. |

Grant `roles/iam.serviceAccountUser` narrowly on the target service account that
the caller must act as. Do not grant broad project-level act-as permissions
unless there is explicit operator evidence and review.

## OIDC And WIF Mental Model

The staging deploy path uses short-lived identity instead of static JSON keys:

```text
GitHub OIDC
  -> GitHub deploy service account
  -> Cloud Build execution service account
  -> Cloud Run runtime service account
```

The GitHub workflow authenticates through Workload Identity Federation and
impersonates the GitHub deploy service account. That account submits the Cloud
Build job and, when configured, acts as the Cloud Build execution service
account. Cloud Build then builds, pushes, and deploys. During Cloud Run deploy,
Cloud Build needs permission to act as the runtime service account that the
service will run under.

`roles/iam.serviceAccountUser` is act-as permission. It does not create an
identity. The service account must already exist; the role only allows the
caller to attach or impersonate that service account in the approved operation.

## GitHub Configuration

GitHub secrets are currently none for the staging deploy workflows. Secret
values live in Secret Manager. GitHub variables store identifiers, public URLs,
resource names, service account emails, and secret names only.

Backend GitHub repository variable keys:

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

Frontend `environment: staging` variable keys:

```text
STAGING_GCP_PROJECT_ID
STAGING_REGION
STAGING_FIREBASE_PROJECT_ID
STAGING_FIREBASE_SITE
STAGING_FRONTEND_URL
STAGING_API_BASE_URL
STAGING_FIREBASE_AUTH_DOMAIN
STAGING_FIREBASE_APP_ID
STAGING_FIREBASE_API_KEY
STAGING_WORKLOAD_IDENTITY_PROVIDER
STAGING_GITHUB_DEPLOY_SERVICE_ACCOUNT
STAGING_CLOUD_BUILD_SERVICE_ACCOUNT_EMAIL
STAGING_BACKEND_CLOUD_RUN_SERVICE
STAGING_BACKEND_CLOUD_RUN_REGION
STAGING_FIREBASE_TOOLS_VERSION
STAGING_E2E_BASE_URL
STAGING_E2E_USER_EMAIL
STAGING_E2E_USER_PASSWORD_SECRET
```

Secret Manager keys:

```text
stds-staging-database-url
stds-staging-resend-api-key
frontend-staging-e2e-password
```

`stds-staging-scheduler-key` is a later scheduler secret name if scheduler jobs
are enabled. Scheduler is excluded from staging v1.

## Workflow Inputs And Substitutions

Backend manual workflow: `.github/workflows/staging-deploy.yml`

`workflow_dispatch` inputs:

```text
ref
image_tag
run_smoke
```

Backend Cloud Build substitutions:

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

Frontend manual workflow:
`/Users/cheni-fan/stds_frontend/.github/workflows/staging-frontend-deploy.yml`

`workflow_dispatch` inputs:

```text
backend_ref
run_e2e
```

Frontend Cloud Build substitutions:

```text
_OPENAPI_SPEC_PATH
_VITE_API_BASE_URL
_VITE_FIREBASE_API_KEY
_VITE_FIREBASE_AUTH_DOMAIN
_VITE_FIREBASE_PROJECT_ID
_VITE_FIREBASE_APP_ID
_VITE_FIREBASE_USE_EMULATOR
_FIREBASE_HOSTING_SITE
_BACKEND_CLOUD_RUN_SERVICE
_BACKEND_CLOUD_RUN_REGION
_FIREBASE_TOOLS_VERSION
```

Use `VITE_API_BASE_URL=/api/v1` for the normal same-origin Firebase Hosting
rewrite contract. If a full backend URL is used instead, record why the
same-origin path is not being used.

## Observability

Use GCP-native observability first: Logs Explorer, Cloud Run logs, Cloud Build
logs, Firebase Hosting visibility where enabled, Cloud SQL metrics, alerts, and
budget.

First-line debugging:

1. Check the GitHub Actions run for input values, selected ref, and failed step.
2. Check the regional Cloud Build build logs when deploy fails before runtime.
3. Check Cloud Run revision, request logs, app logs, and system logs when the
   service deploys but does not serve.
4. Check frontend Playwright artifacts only for E2E failures, and treat them as
   sensitive artifacts.

Cloud Run request/app logs query skeleton:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="stds-staging-core-backend"
resource.labels.location="asia-east1"
```

Follow one request by `request_id`:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="stds-staging-core-backend"
resource.labels.location="asia-east1"
jsonPayload.request_id="<request-id>"
```

HTTP 5xx request logs:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="stds-staging-core-backend"
resource.labels.location="asia-east1"
httpRequest.status>=500
```

Recent application errors:

```text
resource.type="cloud_run_revision"
resource.labels.service_name="stds-staging-core-backend"
resource.labels.location="asia-east1"
severity>=ERROR
```

Cloud Build deploy logs:

- Backend build ID from #202 proven run:
  `d9eda1cc-daed-455b-a407-6b9d35a82d1a`
- Frontend build ID from #202 proven run:
  `1e6c1420-2309-401b-896a-71fed5ae0813`
- Use these as historical evidence only; new deploys must record their own
  build IDs.

Frontend E2E failure path:

- Check the `Run Playwright staging E2E` step first.
- Review `staging-playwright-artifacts` only when needed.
- Do not attach screenshots, videos, traces, or HTML reports to issues before
  reviewing for tokens, emails, user data, and sensitive UI state.

Cloud SQL metrics to check:

- CPU utilization.
- Storage utilization.
- Connection count.
- `max_connections` from the database.
- Pool inference from backend env:
  `DB_MAX_OPEN_CONNS=5`, `DB_MAX_IDLE_CONNS=2`,
  `DB_CONN_MAX_LIFETIME=5m`.

Alert and budget baseline:

- Cloud Run uptime or service unavailable.
- Elevated Cloud Run 5xx count or error ratio.
- Cloud Run instance count reaching configured max instances.
- Cloud SQL CPU, storage, and connections approaching accepted thresholds.
- Staging project budget threshold.

Use human-operated notifications for staging. Do not add production-style pager
routing for the first staging wave.

## Troubleshooting Ledger

| Symptom | Root cause / interpretation | Fix / future check |
| --- | --- | --- |
| GH013 direct push rejection | Protected `dev` branch requires PR and checks. | Use small PRs for deploy fixes. PR #203 and PR #204 followed this path. |
| Cloud Build submit fails before a regional build record exists | Region/source staging directory was implicit, which caused bucket/service usage style failure. | Submit with `--region=asia-east1` and `--gcs-source-staging-dir=gs://stds-439609_cloudbuild/source`. |
| Cloud Build SA builds but cannot deploy Cloud Run | `roles/run.builder` was insufficient for `gcloud run deploy`; failure included `run.services.get`. | Backend Cloud Build execution SA needs Cloud Run deploy permission such as `roles/run.admin`, plus act-as on the runtime SA. |
| DB secret works poorly or non-SSL rejection appears | Old DB secret shape did not satisfy the staging DB SSL requirement. | `stds-staging-database-url` must use the validated shape with `sslmode=require`; never print the full value. |
| Cloud Run revision fails before listening with `stage=create_server` | Failure has moved into app startup, not GitHub Actions, Cloud Build, or Artifact Registry. In #202 this became a DB ping timeout path. | Check Cloud Run app/system logs, private IP path, Cloud SQL connectivity, DB user, and app startup code before adding IAM. |
| `pg_isready` and `psql` succeed but Go app still times out | A single fixed 3 second app startup DB ping was too brittle for Cloud Run cold start plus private Cloud SQL path. | PR #204 changed startup DB ping to bounded retry: 10s per attempt, 3 attempts, 2s delay. |
| Deploy success is treated as staging readiness | Deploy only proves build/push/deploy/startup. It does not prove migrated data, auth, GCS, Resend, or browser E2E. | Continue DB import, auth bootstrap, backend smoke, frontend deploy, frontend E2E, and observability evidence. |
| Operator needs DB inspection/import path | Cloud SQL private IP means public DB IP is not the default operator path. | Use the approved private path, Cloud SQL Console/import, Cloud SQL Studio, DataGrip path, or documented operator route. Do not temporarily enable public IP without exception and expiry. |
| `run.app` URL is reachable | Backend deploy uses `--allow-unauthenticated`; Firebase Hosting is the browser contract, but the default Cloud Run URL may remain reachable. | Keep Firebase Auth and DB-backed authorization as the API security boundary. Treat strict Hosting-only ingress as a later hardening decision. |
| Frontend E2E first runs fail after backend is healthy | #117 and #118 showed selector issues, not backend runtime failures. | Fix frontend selectors and rerun the staging workflow with `run_e2e=true`. Passing run #25923612595 proved the deployed flow. |

## Evidence And Redaction Policy

Allowed evidence:

- GitHub Actions run URL, selected ref, image tag, and non-secret inputs.
- Cloud Build build ID and final status.
- Artifact Registry image path and tag.
- Cloud Run service, revision, runtime service account, ingress, VPC/subnet,
  min/max instances, and redacted env summary.
- Secret Manager secret names and IAM binding summaries without values.
- Cloud SQL instance/database names, private IP status, metric summaries,
  imported migration version, validation summary, and database user names only.
- Firebase Hosting URL, site, release metadata, and `/api/**` rewrite target.
- Playwright result summary and redacted artifact links when needed.
- Logs Explorer filters and redacted request/app log examples.
- Alert policy metadata and billing budget summary.

Never include:

- Tokens, bearer headers, Firebase ID tokens, or service account JSON.
- Passwords or E2E user password.
- Full `DATABASE_URL` or database passwords.
- Secret Manager values.
- Signed upload/download URLs or signed URL query strings.
- Raw request/response payloads.
- Raw Firebase, Resend, GCS, database, or provider error payloads.
- PII-heavy screenshots, videos, traces, or HTML reports.
- Dump contents, private GCS object URLs, or database import artifacts.

## Source References

- Backend issue: ifan0927/STDS_backend_go#202.
- Backend PRs: #203 for explicit regional Cloud Build submit/source staging
  dir; #204 for bounded startup DB ping retry.
- Frontend fixes referenced by #202: ifan0927/stds_fronted#117 and
  ifan0927/stds_fronted#118.
- Backend repo docs/workflows:
  - `docs/staging-runbook.md`
  - `docs/cloud-architecture.md`
  - `.github/workflows/staging-deploy.yml`
  - `cloudbuild.staging.yaml`
- Frontend repo docs/workflows:
  - `/Users/cheni-fan/stds_frontend/docs/staging-environment-contract.md`
  - `/Users/cheni-fan/stds_frontend/docs/staging-observability-baseline.md`
  - `/Users/cheni-fan/stds_frontend/.github/workflows/staging-frontend-deploy.yml`
  - `/Users/cheni-fan/stds_frontend/cloudbuild.staging.yaml`
