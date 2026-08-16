# ADR 0003: Cloud Architecture Baseline Status

- Status: Proposed for production; verified for the documented staging/UAT slice only
- Last verified: 2026-07-10

## Context

Repo已有Cloud Run/Cloud Build/Cloud SQL staging部署與runbook證據，但production sizing、HA、backup/PITR、ingress、operator access、cutover與recovery objectives仍未確認。

## Decision

[`../../cloud-architecture.md`](../../cloud-architecture.md)是cloud architecture baseline，不是production final design。[`../../staging-runbook.md`](../../staging-runbook.md)只約束first staging demo/UAT slice。Production必須通過獨立readiness與cutover sign-off，不能由staging成功推定。

## Consequences

- Staging runbook不得承諾production RTO/RPO、rollback或data migration guarantees。
- Cloud SQL tier、HA、PITR、minimum instances、ingress與production deployment flow維持proposed/TBD。
- Production gaps集中在[`../../production-readiness.md`](../../production-readiness.md)。
