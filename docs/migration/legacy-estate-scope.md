# Legacy Estate Migration Scope

狀態：current as-is + production gaps，驗證日期2026-07-10。

## Current Importer

| Scope | Status | Production note |
| --- | --- | --- |
| Properties / placeholder owners | implemented | 必須reconcile真實owner與登入身份 |
| Rooms | implemented | orphan policy與source completeness仍需sign-off |
| Tenants | implemented | natural-person dedupe與co-tenant unresolved |
| Leases | partial | cadence、starting meter、actual move-out與legacy-only semantics需reconcile |
| Room status | implemented reconciliation | 仍需驗overlapping lease與orphan policy |
| Electricity/rent bills | risky as-is | meter history與dates不能單獨證明paid/overdue |
| Journal / repair | partial | actor、reply flattening、accounting與attachments有loss/gaps |
| Legacy mapping tables | implemented | 需擴充所有production entities與approved loss reasons |
| Final validation | partial | 現況主要證明target consistency，尚非source completeness gate |
| Estate-linked accounting | not implemented | production blocker |
| Active identities/property assignments | not implemented | production blocker |
| Attachment metadata/object copy | not implemented | production blocker |

現有foundation應延伸而非推倒重寫；它可支援dev/UAT，但不能宣告production-ready。

## Mapping Classification

每一類source record與field都必須在mapping spec標示下列其中一種：

- `confirmed mapping`：domain semantics已確認，且target能無損表達。
- `as-is importer`：程式目前如此處理，但business truth尚未確認。
- `approved loss`：使用者已核准不搬或降階保存，具有stable reason code。
- `unresolved loss`：仍待決策；production blocking validation必須失敗。
- `out of scope`：與estate migration無關，且範圍經明確界定。

不得用`default`、`unknown`或placeholder掩蓋unresolved loss。Placeholder identity若保留，必須逐筆核准或在production gate歸零。

## Accounting And Payment Truth

Legacy meter readings證明用量，不證明付款；日期經過也不證明欠租。Rent、electricity、deposit、refund、checkout與journal expense狀態必須以納入scope的estate-linked accounting evidence與已確認mapping重建。

在該mapping確認前：

- 不得把歷史meter row默認成paid electricity bill。
- 不得只因due date早於migration date就默認成overdue rent。
- accepted loss與unresolved loss必須分開計數。

## Additional Decision Backlog

除canonical [`../design/decision-backlog.md`](../design/decision-backlog.md)外，migration還需確認：facility data、reply representation與actor fallback、orphan handling、early/continue/pet semantics、第一筆與vacancy-period meter readings。未確認前只能列為unresolved，不能寫進canonical domain rules。
