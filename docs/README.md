# Documentation Authority

本目錄記錄 STDS Backend 的現行契約、已確認規則、設計決策與操作方式。文件發生衝突時，不以檔名、更新日期或 roadmap checkbox 判定真偽，而依下列權威層級處理。

## Authority Order

1. 已由使用者確認的 business rules：[`design/domain-rule.md`](design/domain-rule.md)。
2. API transport contract：[`spec/src/openapi.yaml`](spec/src/openapi.yaml) 與其 `$ref` 檔案。
3. Database as-is schema：`internal/platform/database/migrate/migrations/*.up.sql`；[`spec/schema.sql`](spec/schema.sql) 只作為可讀 reference。
4. 現行 application/domain/router behavior 與能直接證明該行為的 tests。
5. 有效 ADR：[`design/decisions/`](design/decisions/)。ADR 不得覆蓋已確認 business rule。
6. Current-state inventory：[`current-state.md`](current-state.md)，用來區分 implemented、partial、reserved 與 not implemented。
7. Operations runbooks 與 engineering guidelines；只約束其明示的環境或工作流程。
8. Historical/provisional 文件與 Git history；只能解釋背景，不能建立現行契約。

若 implementation 與 canonical rule 衝突，implementation 只證明 as-is，不會自動改寫 domain truth。衝突必須記入 [`design/decision-backlog.md`](design/decision-backlog.md)，再由後續工作決定修 code 或修 contract。

## Document Inventory

| 文件／路徑 | 類型 | 狀態 | 責任 |
| --- | --- | --- | --- |
| `docs/README.md` | policy | canonical | 文件責任、權威順序與 inventory |
| `docs/design/domain-rule.md` | domain | canonical | 只收已確認且可約束 schema/API 的 business rules |
| `docs/design/domain-model.md` | design | current as-is | bounded contexts、aggregates、states、lifecycles 與跨 BC 邊界 |
| `docs/design/decisions/*.md` | ADR | status per file | 仍有效或已被取代的技術／架構決策 |
| `docs/design/decision-backlog.md` | decision queue | canonical index | 未確認或具風險、不得默認成規則的議題 |
| `docs/current-state.md` | inventory | current as-is | implementation、OpenAPI、migration 與 tests 的功能狀態 |
| `docs/spec/src/**` | API source | canonical | modular OpenAPI source of truth |
| `docs/spec/openapi.yaml` | generated | derived | bundle output；不得手動維護 |
| `internal/http/api/openapi.gen.go` | generated | derived | OpenAPI code generation output |
| `internal/platform/database/migrate/migrations/**` | schema source | canonical as-is | migration order與實際 DB schema |
| `docs/spec/schema.sql` | schema reference | derived/reference | 人工可讀 schema snapshot；衝突時以 migrations 為準 |
| `docs/migration/**` | migration | canonical contract | 可版本控制的 scope、mapping、validation、evidence 與 cutover contract |
| `docs/mirgations/**` | migration input | ignored/private fixture | legacy JSON 與舊 task plan；不是 tracked contract（路徑拼字為歷史遺留） |
| `artifacts/**` | generated/private | ignored | dump、report、run evidence；不得加入 Git |
| `docs/.rules/**` | engineering rules | canonical | coding 與 testing 規範 |
| `docs/local-operations.md` | runbook | current/local | local、E2E、CI 與開發用 migration 操作 |
| `docs/staging-runbook.md` | runbook | current UAT only | staging demo/UAT；不是 production contract |
| `docs/production-readiness.md` | readiness | unresolved | production sign-off、rollback 與 RTO/RPO gaps |
| `docs/cloud-architecture.md` | architecture | proposed baseline | 已驗證 staging 基線與 production 待決項目；非 production final design |
| `docs/spec/tech-stack.md` | technical decisions | current/provisional | 已採用技術與仍待 production 確認的選項 |
| `docs/spec/tasks.md` | roadmap | historical | 舊實作切片；checkbox 不代表現況或完成度 |
| `docs/vertical-slice-handbook.md` | handbook | historical example | 早期 slice 教學；工程規範以 `docs/.rules/**` 為準 |
| `docs/wiki-drafts/**` | draft | historical/provisional | operator 草稿，不是部署或 production contract |
| `docs/reportfile/**` | binary reference | local/untracked | legacy 報表樣本；不得視為 API/domain contract |
| `README.md` | repository overview | current descriptive | 導覽與高階現況；具體contract仍依本頁權威順序 |
| `CLAUDE.md` | agent context | tooling guidance | 不得覆蓋`AGENTS.md`、canonical rules或machine contracts |
| `cicd.md` | delivery notes | historical/provisional | 早期CI/CD規劃；現行pipeline以`.github/workflows/**`為準 |

## Maintenance Rules

- 新 business rule 只有在使用者明確確認後才能寫入 `domain-rule.md`。
- OpenAPI 先改 `docs/spec/src/**`，再 bundle/generate；不直接改 generated output。
- Schema 變更先新增 DB migration，再更新 `docs/spec/schema.sql` reference。
- Roadmap、runbook、ADR、as-is model 不可用「規則」語氣替代未確認 business decision。
- Production data、PII、token、credential、dump、migration JSON 與 runtime report 不得加入 tracked docs。
