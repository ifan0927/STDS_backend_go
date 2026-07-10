# Tech Stack

- Status: current implementation + proposed production settings
- Last verified: 2026-07-10

本文分開描述repo已採用技術與尚未確認的production infrastructure。即時版本/行為以`go.mod`、Dockerfile、workflows、DB migrations與runtime wiring為準。

## Current Implementation

### Language And Runtime

- Go 1.24（`go.mod`與Dockerfile builder）。
- Gin HTTP framework。
- Single deployable monolith；bounded contexts是code ownership boundaries，不是microservices。
- OpenAPI-first：source在`docs/spec/src/**`，bundle與Go bindings由script產生。

### Persistence

- PostgreSQL + `database/sql`，主要driver為pgx stdlib。
- SQL migrations是physical schema source of truth。
- Repositories/query adapters使用explicit SQL；repo沒有GORM runtime dependency。
- Transactions由application use case與`txrunner`協調；business rules不放進DB triggers。
- `docs/spec/schema.sql`是derived/reference snapshot，不是第二套schema authority。

### Authentication And Authorization

- Firebase Auth驗證ID token與外部身份。
- Backend以`firebase_uid`載入active DB user principal。
- Runtime RBAC/property scope使用DB `role`、`assigned_property_ids`與property ownership。
- Firebase Custom Claims可以同步，但不覆蓋DB principal。
- `permission_overrides`目前只保存/回傳，正式授權語意尚未確認。

### Domain Events

- Synchronous in-process dispatcher，publish發生在command DB transaction commit之後。
- 無outbox、retry、dead-letter queue或durable delivery。
- 強一致的核心/財務side effects使用同transaction direct orchestration。
- Lease room/tenant lifecycle仍使用post-commit subscribers，列為current consistency risk。

### External Services

- Email：Resend adapter；password reset/onboarding與部分scheduler通知已實作。
- LINE：只有stub，runtime未實作。
- Attachments：Google Cloud Storage signed upload URLs與metadata verification；test/e2e可使用受限fake mode。
- Observability：structured `slog`、request middleware、Cloud Logging/Monitoring compatible exporters。

### Brand Public Read Boundary

- Core backend public namespace只讀approved PostgreSQL views。
- Public handlers不得直接查詢tenant、lease、bill、attachment、deposit/accounting或其他未核准base tables。
- 現行brand architecture不需要獨立`brand_readonly`production principal；repo保留ephemeral-role contract tests與local diagnostic helper。

### CI And Staging Deployment

- PR/push gates由GitHub Actions執行Go tests/build、OpenAPI lint/generate diff與migration checks。
- Staging deployment intent來自`staging` branch或controlled manual dispatch；GitHub Actions提交Cloud Build，建置Artifact Registry image並部署Cloud Run。
- Admin frontend使用Firebase Hosting；brand frontend是另一repo的Cloudflare Pages deployment。
- Scheduler use cases與protected HTTP endpoints已存在；first staging demo runbook不包含Cloud Scheduler provisioning。

## Proposed Production Baseline

下列項目不是final production contract：

- Cloud Run region、min/max instances、CPU/memory與ingress policy。
- Cloud SQL tier、HA、backup window、PITR、maintenance與connection budget。
- Production promotion workflow、operator DB access、rollback與alert ownership。
- Cloud Scheduler job provisioning與failure operations。
- Monthly cost estimates。

Production決策與證據集中在[`../production-readiness.md`](../production-readiness.md)；staging成功不能替代production sign-off。

## Stable Engineering Choices

- Keep the service monolithic unless a separately confirmed operational need justifies a deployment split.
- Keep API, domain, persistence and generated models separate.
- Use read/query repositories for projections and application services for commands.
- Keep Firebase responsible for identity and DB principal responsible for authorization.
- Keep strong state changes inside the command transaction; do not add critical post-commit subscribers without an explicit reliability decision.
