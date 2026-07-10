# Current-State Inventory

狀態日期：2026-07-10。本文只描述 repo 現況，不判定 implementation 是否為最終 domain truth。完成度由 OpenAPI source、DB migrations、application/domain/router 與 tests 交叉驗證，不使用 [`spec/tasks.md`](spec/tasks.md) checkbox。

## Status Definitions

- `implemented`：route/use case/persistence 已存在，且有相符的 contract 或 focused tests。
- `partial`：主要路徑存在，但必要 side effect、production guarantee 或完整 use case 尚缺。
- `reserved`：schema、event 或 contract 有預留，但 runtime 沒有承諾對應行為。
- `not implemented`：文件曾描述或 production 需要，但 repo 沒有可執行路徑。

## Functional Inventory

| Bounded context／能力 | 狀態 | As-is 證據與限制 |
| --- | --- | --- |
| Identity authentication | implemented | Firebase 驗 ID token；middleware 再以 `firebase_uid` 載入 DB user principal。 |
| RBAC + property scope authorization | implemented | Router policy、property resolver 與 DB principal 的 `role` / `assigned_property_ids` 執行授權；owner scope 另以 property ownership 判斷。 |
| Custom Claims synchronization | partial | Firebase adapter可寫 claims，但 runtime authorization 仍以 DB principal 為準；`permission_overrides` 的正式 domain 語意未確認。 |
| User lifecycle / property assignments / password reset | implemented | OpenAPI、application services、Firebase/email adapters 與 focused tests 已存在。 |
| Properties / rooms / dashboards / public availability | implemented | CRUD/read models、room maintenance入口與 approved public availability view 已存在。 |
| Room recovery after repair completion/cancellation | implemented | Repair complete/cancel在同一transaction執行room recovery；只有該room沒有其他active repair時才恢復vacant。Events只保留trace。 |
| Tenants / leases / cadence / bill pre-generation | implemented | Tenant、lease、replacement、rent/electric cadence 與預產 bill use cases 已存在。 |
| Lease termination room/tenant status follow-up | partial | `LeaseCreated` / `LeaseTerminated` 以 synchronous post-commit subscriber 更新 room/tenant；失敗無 durable retry，可能與 lease state 不一致。 |
| Lease replacement meter baseline | not implemented decision | Replacement流程存在，但新 lease meter baseline 的正式規則未確認。 |
| Checkout preview/finalize/export | implemented | Backend preview token、transaction內重算/finalize、persisted settlement snapshot與 HTML export 已存在。Amendment/reopen 未實作。 |
| Bills / meter / payment / receipts | implemented | Meter計價、收款與 receipt 路徑已存在；payment/accounting entry在同一 transaction。 |
| Force termination | partial/risky | Runtime會 write off 未結帳 bills 並處理押金，但「哪些 bills 可 write off」仍是未確認 production domain decision。 |
| Property accounting / financial reports / snapshots | implemented | Accounting titles、entries、月結 snapshot、report/read/export 路徑已存在。月結後補帳與 amendment 未實作。 |
| Journal logs / repair workflow | implemented | Journal、費用accounting、repair assign/progress/complete/cancel與conditional room recovery已存在。 |
| Attachments | implemented core / partial operations | Signed upload、register、list、download與多資源關聯已存在；upload-token/GCS cleanup jobs未實作，retention、ID copy、hard-delete與legacy object mapping尚未確認。 |
| Scheduler endpoints | implemented six jobs | Lease expiry、lease expiring soon、force termination compensation、overdue scan/reminder與monthly snapshots受保護endpoint已存在；production schedule/operations仍需部署確認。 |
| Email notifications | partial | Onboarding/password reset與部分 scheduler通知有 direct flow；部分 published events只有 trace/reserved，財報 send event沒有 delivery subscriber。 |
| Brand management / public brand reads | implemented | Brand profile/FAQ admin paths與 approved-view public reads 已存在；brand site deployment屬另一 repo。 |
| Legacy estate migration | partial / UAT fixture | Properties、rooms、tenants、leases、room status、bills、journal與 validation stages 已存在；accounting、identity/property assignment、attachments與 production completeness仍缺。 |
| Production cutover | not implemented contract | Write freeze/delta、final sign-off、rollback及 RTO/RPO 尚未確認。 |

OpenAPI目前包含75個paths與103個operations；generated server interface與router policies各有103個對應handler/policy，結構上完整對齊。

## Contract Boundaries

- OpenAPI source 是 `docs/spec/src/**`；`docs/spec/openapi.yaml` 與 `internal/http/api/openapi.gen.go` 是 derived output。
- DB schema 的執行證據是 `internal/platform/database/migrate/migrations/*.up.sql`；`docs/spec/schema.sql` 是 reference snapshot。
- API 中沿用的 `BR-*` 標籤描述目前 transport/runtime contract；若該議題列在 [`design/decision-backlog.md`](design/decision-backlog.md)，不代表 production domain decision 已確認。
- `docs/mirgations` 的 JSON 與 `artifacts` report 是 ignored development/UAT input/evidence，不是版本控制的完成證明。

## Known Consistency Risks

1. Lease建立/終止後的 room與tenant核心狀態透過 post-commit event subscriber更新；commit後 subscriber失敗無 rollback或durable retry。
2. Legacy migration把部分 meter/payment/rent狀態從不足的來源推導；這只能視為現行 importer行為，不能作為歷史付款真相。
3. Final legacy validator可證明部分 target內部一致性，但尚未全面強制 `source = mapped + approved skipped`。
4. `permission_overrides` 已存在資料欄位與API模型，但正式合併、優先順序及限制語意尚未成為 canonical rule。
