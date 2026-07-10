# Production Readiness

- Status: Unresolved
- Scope: production only

Staging runbook只證明first demo/UAT slice。Production deployment、migration、recovery與service objectives必須另行確認。

## Required Sign-Off Areas

| Area | Current status | Production decision/evidence required |
| --- | --- | --- |
| Cloud SQL sizing/HA | proposed | Tier、HA topology、connection limits與load evidence |
| Backup/PITR | unresolved | Retention、restore test、operator access與measured restore time |
| RTO/RPO | unresolved | Approved objectives與rehearsal results |
| Ingress/auth hardening | partial | Public/default URL policy、frontend origins、scheduler/admin access |
| Deployment promotion | staging-only | Production branch/environment approval、image provenance、rollback |
| Observability/on-call | partial | Alerts、owner、runbooks、redaction與incident escalation |
| Legacy migration | not ready | [`migration/validation-contract.md`](migration/validation-contract.md)與cutover decisions |
| Attachments/PII | unresolved | Retention、ID copy、hard-delete、object migration與access review |
| Data rollback | unresolved | Write freeze/delta/divergence/restore or roll-forward contract |

Production readiness不能由staging smoke、successful deploy或UAT fixture validation單獨推定。
