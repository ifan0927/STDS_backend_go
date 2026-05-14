# STDS Cloud Architecture

Project: STDS - Property Management System
Version: 1.0
Status: Proposed baseline for staging; production use requires a separate
production readiness review.

## Overview

STDS runs on GCP using Firebase Hosting, Cloud Run, Cloud SQL, Secret Manager,
Cloud Storage, Cloud Scheduler, and Cloud Logging / Monitoring.

This document is the deployment architecture baseline for the staging line and
the early production candidate. It intentionally avoids an external Application
Load Balancer in the first version. Firebase Hosting is the browser-facing edge
for frontend traffic, while Cloud Run hosts backend services.

Cloud SQL private IP and Cloud Run Direct VPC egress are included in staging v1.
This is a security hardening choice because legacy property data will be
migrated early. It is not treated as an HTTP ingress boundary.

## Core Decisions

### No External Application Load Balancer In The First Version

Firebase Hosting handles browser-facing TLS, custom domains, CDN behavior, and
frontend rewrites. API security remains enforced by Firebase Auth, DB-backed
authorization, service account IAM, and scheduler keys.

An external Application Load Balancer is a future option for a stronger
production edge, Cloud Armor, host/path routing across many services, WAF,
rate limiting, or strict Cloud Run ingress through the load balancer. It is out
of scope for the staging deployment line.

### Firebase Hosting Rewrites For Browser-To-Backend Calls

Admin and brand frontends should call backend APIs through Firebase Hosting
rewrites where practical:

- admin frontend: `/api/**` to the core backend Cloud Run service.
- brand frontend: `/brand-api/**` to the brand thin backend Cloud Run service.

The Cloud Run `run.app` URL is not part of the browser-facing contract. Whether
the default `run.app` URL can be disabled safely is a separate hardening
decision and must be verified before relying on it. Firebase Hosting rewrites
must be tested against the chosen Cloud Run ingress/default-URL settings.

### VPC With Direct VPC Egress For Cloud SQL

Cloud Run services should connect to Cloud SQL through private IP using Direct
VPC egress if the staging operator accepts the added setup complexity.

Direct VPC egress is preferred over Serverless VPC Access connector for this
project because it avoids fixed connector VM cost and scales with Cloud Run
traffic. The tradeoff is extra networking setup and a more complex migration /
operator access path.

VPC/private IP is used for backend-to-database connectivity. It is not the
browser ingress boundary.

### One Cloud SQL Instance, Separate Databases

The cost-conscious baseline is one Cloud SQL PostgreSQL instance with separate
databases:

- `stds_staging`
- `stds_production`

Each environment uses a separate DB credential scoped to its own database.

This is an accepted cost tradeoff, not a hard security boundary. Staging and
production share instance-level CPU, memory, connection, maintenance, and
availability behavior. A staging migration, bad query, or connection spike can
affect the shared instance. If this risk becomes unacceptable, split staging and
production into separate Cloud SQL instances.

### Brand Thin Backend Isolation

The brand-facing backend is a separate Cloud Run service with its own service
account and DB credential. It may only query approved read-only views:

- `approved_brand_profile_v1`
- `approved_brand_faq_items_v1`
- `approved_brand_property_availability_v1`

It must not have direct access to core base tables such as tenants, leases,
bills, deposits, accounting, attachments, or operational property/room tables
except through explicitly approved views.

### Explicit DB Connection Pool Limits

Cloud Run can scale horizontally. Each instance owns its own DB connection
pool, so small Cloud SQL instances can hit connection pressure before CPU
becomes the bottleneck.

The runtime should support environment-controlled pool settings:

```text
DB_MAX_OPEN_CONNS
DB_MAX_IDLE_CONNS
DB_CONN_MAX_LIFETIME
```

The Go runtime reads these values from the environment and applies them to the
`database/sql` pool when opening the PostgreSQL connection.

Initial target values:

```text
staging:
  DB_MAX_OPEN_CONNS=5
  DB_MAX_IDLE_CONNS=2
  DB_CONN_MAX_LIFETIME=5m

production candidate initial:
  DB_MAX_OPEN_CONNS=5
  DB_MAX_IDLE_CONNS=2
  DB_CONN_MAX_LIFETIME=5m
```

The actual Cloud SQL `max_connections` value must be verified on the created
instance, for example through `pg_settings`, instead of being hardcoded in this
document.

## Architecture

### High-Level Runtime

```mermaid
flowchart TD
    AdminUser[Admin / staff browser] --> AdminHosting[Firebase Hosting<br/>admin domain]
    PublicUser[Public visitor] --> BrandHosting[Firebase Hosting<br/>brand domain]

    AdminHosting -->|/api/** rewrite| CoreRun[Cloud Run<br/>Core Backend]
    BrandHosting -->|/brand-api/** rewrite| BrandThinRun[Cloud Run<br/>Brand Thin Backend]

    Scheduler[Cloud Scheduler] -->|X-Scheduler-Key| CoreRun

    subgraph GCPProject[GCP Project]
        CoreRun
        BrandThinRun

        subgraph PrivateDataPath[Private DB Connectivity]
            DirectVPC[Direct VPC egress]
            VPC[VPC network]
            CloudSQL[(Cloud SQL PostgreSQL<br/>private IP)]
        end
    end

    CoreRun --> DirectVPC
    BrandThinRun --> DirectVPC
    DirectVPC --> VPC
    VPC --> CloudSQL

    CoreRun -->|generate signed URL / metadata check| GCS[(Cloud Storage<br/>attachments bucket)]
    AdminUser -->|PUT signed URL| GCS

    CoreRun --> FirebaseAuth[Firebase Auth]
    AdminHosting --> FirebaseAuth

    CoreRun --> SecretManager[Secret Manager<br/>env injection]
    BrandThinRun --> SecretManager

    CoreRun --> Resend[Resend Email API]

    CoreRun --> Observability[Cloud Logging / Monitoring]
    BrandThinRun --> Observability
```

### Browser API Flow

```mermaid
sequenceDiagram
    participant Browser
    participant Hosting as Firebase Hosting
    participant API as Cloud Run Core Backend
    participant Auth as Firebase Auth
    participant DB as Cloud SQL

    Browser->>Auth: Sign in / obtain ID token
    Browser->>Hosting: Load admin app
    Hosting-->>Browser: Static assets
    Browser->>Hosting: /api/v1/... with bearer token
    Hosting->>API: Rewrite request to Cloud Run
    API->>Auth: Verify Firebase ID token
    API->>DB: Load DB principal / execute request
    API-->>Browser: API response
```

### Attachment Upload Flow

```mermaid
sequenceDiagram
    participant Browser
    participant API as Cloud Run Core Backend
    participant GCS as Cloud Storage

    Browser->>API: POST /attachments/upload-url
    API-->>Browser: Signed upload URL
    Browser->>GCS: PUT file to signed URL
    Browser->>API: Register attachment
    API->>GCS: Read object metadata
    API-->>Browser: Attachment response
```

GCS is not routed through VPC or Firebase Hosting. Browser upload uses scoped
signed URLs. The bucket CORS policy must allow only approved frontend origins.

### Scheduler Flow

```mermaid
sequenceDiagram
    participant Scheduler as Cloud Scheduler
    participant API as Cloud Run Core Backend
    participant DB as Cloud SQL

    Scheduler->>API: POST /api/v1/internal/jobs/... ?window_key=YYYY-MM-DD<br/>X-Scheduler-Key
    API->>API: Validate scheduler key
    API->>DB: Acquire scheduler_job_runs row
    API->>DB: Execute job logic
    API-->>Scheduler: 202 accepted / skipped / failed
```

All browser traffic enters through Firebase Hosting. Cloud Scheduler is not
browser traffic and may call scheduler endpoints directly.

## Service Responsibilities

### Core Backend

- Serves admin APIs.
- Validates Firebase Auth bearer tokens.
- Resolves DB-backed user principal.
- Enforces RBAC and property-scoped middleware.
- Executes scheduler jobs.
- Generates GCS signed URLs and checks attachment metadata.
- Dispatches transactional email through Resend.
- Connects to Cloud SQL through the chosen private connectivity path.
- Receives secrets through Cloud Run environment variables backed by Secret
  Manager.
- Starts with `max-instances=2` to bound DB connection pressure.

### Brand Thin Backend

- Serves public brand and property availability APIs.
- Uses its own Cloud Run service account.
- Uses its own readonly DB credential.
- Reads approved views only.
- Does not use the core backend runtime DB credential.

### Firebase Hosting

- Hosts admin frontend and brand frontend.
- Provides custom domains and TLS for browser traffic.
- Rewrites API paths to Cloud Run services.
- Does not replace backend authentication or authorization.

### Cloud SQL

- PostgreSQL.
- Target machine type for the cost-conscious production candidate:
  `db-g1-small`.
- `db-f1-micro` remains acceptable for staging or low-risk demo environments,
  but `db-g1-small` is preferred for early production candidate stability.
- Daily automated backups.
- Seven-day backup retention.
- PITR enabled for production before go-live; optional for staging.
- No HA in the first version unless a production uptime requirement is
  accepted.

### Secret Manager

Secret Manager is the source of runtime secret values. Cloud Run injects those
secrets into environment variables where the current Go runtime reads them.
Application code should not require direct Secret Manager API calls for the
first version.

Secret values must never be committed or printed in logs.

## Observability

### Logging

Cloud Run automatically sends stdout/stderr, request logs, and system logs to
Cloud Logging. The current Go runtime uses structured JSON logging with
`log/slog`.

Important fields:

- `request_id`
- `user_id` when available
- `property_id` when available
- `method`
- `path`
- `status`
- `latency`
- scheduler `job_key`
- scheduler `window_key`
- scheduler result

Do not log bearer tokens, scheduler keys, DB passwords, private keys, or full
request/response bodies containing sensitive data.

### Monitoring

Use GCP-native monitoring in the first version. Do not introduce Prometheus or
Grafana for staging.

Baseline metrics:

- Cloud Run request count.
- Cloud Run latency.
- Cloud Run 5xx/error count.
- Cloud Run instance count.
- Cloud SQL CPU.
- Cloud SQL connection count.
- Cloud SQL storage usage.

Baseline alerts:

- Cloud Run service unavailable.
- Elevated Cloud Run 5xx/error count.
- Cloud Run instance count reaches `max-instances`.
- Cloud SQL CPU over threshold.
- Cloud SQL connection count approaching actual `max_connections`.
- Cloud SQL disk usage over threshold.
- Billing budget alert.

Log-based metrics are optional for staging. If added, keep them narrow:

- scheduler failed results.
- auth validation failures.
- signed URL generation count.

## Security Posture

No single layer is the only security boundary.

| Layer | Control |
| --- | --- |
| Network | Cloud SQL private IP; no public DB endpoint in the target architecture. |
| Browser ingress | Firebase Hosting custom domains and rewrites. |
| Authentication | Firebase Auth bearer token on admin APIs. |
| Authorization | DB-backed principal, RBAC, and property-scoped middleware. |
| Brand data boundary | Readonly DB principal and approved views only. |
| Secrets | Secret Manager values injected into Cloud Run env vars. |
| Storage | Browser receives scoped signed URLs, not broad GCS credentials. |
| Scheduler | `X-Scheduler-Key` for internal job endpoints. |
| IAM | Separate Cloud Run service accounts with least privilege. |

Accepted tradeoffs:

| Tradeoff | Rationale |
| --- | --- |
| No external Application Load Balancer | Current scale does not justify cost or operational complexity. |
| No Cloud Armor / WAF | Revisit when public abuse or stronger production edge controls are needed. |
| One Cloud SQL instance for staging and production | Cost-conscious choice; instance-level resource contention is accepted initially. |
| No HA in first version | Acceptable until a production uptime SLA is required. |
| Cloud Run default URL disabling is not assumed | Hosting rewrites hide the URL from browser contracts, but default URL hardening must be verified separately. |

## Cost Baseline

Approximate low-traffic monthly cost:

| Component | Monthly estimate |
| --- | --- |
| Cloud SQL `db-g1-small` | 25-30 USD |
| Cloud SQL storage / backups | 2-5 USD |
| Cloud Run | 0-2 USD |
| Firebase Hosting | 0 USD in free-tier scale |
| Secret Manager | 0-1 USD at current scale |
| Cloud Storage attachments | 0-1 USD initially |
| Direct VPC egress | traffic-proportional; expected low at current scale |
| Cloud Logging / Monitoring | 0-2 USD if logs stay modest |
| Cloud Scheduler | about 0.30 USD for six jobs after free jobs |

Cloud SQL is expected to dominate the cost profile. Cost assumptions must be
verified with GCP Billing and the Pricing Calculator before production go-live.

## Deployment Phases

### Staging v1

- Firebase Hosting frontend.
- Cloud Run core backend.
- Cloud SQL private IP with VPC / Direct VPC egress.
- Secret Manager env injection.
- GCS signed URL upload.
- Cloud Logging / Monitoring baseline.
- Cloud Run `max-instances=2`.
- DB pool limits implemented and configured.
- Staging database preparation covers schema migrations, legacy data import,
  legacy validation, and first admin Firebase/DB user bootstrap.
- Smoke checks for deployment, database preparation, Firebase Auth, signed URL
  upload, and log visibility.
- Cloud Scheduler jobs are excluded from the first staging demo wave.
- Deployment starts as a manual GitHub Actions trigger that runs predeploy
  checks, then submits Cloud Build for image build, Artifact Registry push, and
  Cloud Run deploy. Automatic deployment on `staging` branch push is a later
  hardening step after the first setup evidence is reviewed.

### Production Readiness Review

Before production go-live:

- Confirm Cloud SQL machine type.
- Confirm backup window and PITR.
- Confirm whether HA is required.
- Confirm DB pool limits under realistic load.
- Confirm Cloud Build migration connectivity to private Cloud SQL.
- Confirm operator DB access path without public DB IP.
- Confirm whether legacy data import remains a manual operator step or moves
  into a controlled deployment workflow.
- Confirm Firebase Hosting rewrite behavior and whether Cloud Run default URL
  disabling is safe.
- Confirm GCS CORS for production frontend origins.
- Confirm Cloud Monitoring alert policies and billing budgets.

## Repo Impact

Expected repo changes for this architecture:

- Dockerfile and `.dockerignore`.
- Cloud Build staging deployment config.
- GitHub Actions trigger/status workflow if needed.
- Staging database preparation runbook covering schema migration, legacy data
  import, validation, and first admin bootstrap.
- DB pool config support.
- Staging smoke script.
- Deployment runbooks and verification evidence templates.

Application domain logic should not need broad runtime changes for the staging
deployment line.

## Open Decisions

- Should staging and production share one Cloud SQL instance initially?
- How will Cloud Build migrations reach private Cloud SQL?
- How will operators inspect the database without enabling public IP?
- Should legacy data import stay manual for staging v1, or become a controlled
  Cloud Build/operator workflow after the first demo wave?
- Should production require Cloud SQL HA before go-live?
- Should Cloud Run default `run.app` URLs be disabled, and does that work with
  the selected Firebase Hosting rewrite path?
- When should the brand thin backend be deployed relative to core backend
  staging?
