# Accepted Migration Losses

- Status: Canonical registry
- Production accepted losses: none recorded as of 2026-07-10

本文件只記錄使用者已明確核准的legacy data loss或降階保存。未確認項目不得放在這裡，應留在[`../design/decision-backlog.md`](../design/decision-backlog.md)或[`legacy-estate-scope.md`](legacy-estate-scope.md)。Importer目前跳過、flatten、default或placeholder的行為不等於accepted loss。

## Required Entry Shape

每一筆accepted loss必須包含：

- Stable reason code。
- Source entity/field或record class，不放production PII。
- 核准的loss/transform語意與理由。
- 對finance、history、authorization、attachments與reporting的影響。
- Blocking validation如何計數`approved_skipped_count`。
- 使用者確認日期或private approval reference。

在第一筆明確核准前，本registry保持空白；production validation不得把任何skip計入`approved_skipped_count`。
