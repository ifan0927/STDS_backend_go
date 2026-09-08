# Domain Decision Backlog

以下議題尚未被確認，或雖有現行 implementation 但存在 production 風險。它們不是 [`domain-rule.md`](domain-rule.md) 的延伸，也不得從 schema、OpenAPI或 importer現況反推答案。

| 議題 | 類型 | 已知 as-is／風險 | 確認前不得假設 |
| --- | --- | --- | --- |
| 歷史付款真相與 legacy accounting mapping | shared cross-module | Estate accounting尚未納入 importer；meter/date不足以證明付款 | paid、overdue、押金與收支狀態 |
| 提前退租的未來帳單 | Leasing/Billing | runtime有預產帳單與多種終止流程 | 哪些 void、保留、結算或退款 |
| 強制終止可 write off 範圍 | Leasing/Billing | runtime會處理未結帳單，但語意過寬 | 哪些 bill type、period與狀態可列呆帳 |
| Lease replacement meter baseline | Leasing/Billing | replacement存在，baseline未定義 | 沿用舊讀數、要求新讀數或其他策略 |
| 共同承租人 | Leasing | current schema一份租約只連一位tenant；live legacy有雙承租 | 忽略第二人、合併身份或新增關聯模型 |
| 月結後補帳、重開與 amendment | Billing | snapshot/finalized settlement視為不可變 | 更正方式、版本、沖銷與重開權限 |
| Tenant去重與自然人identity | Leasing | legacy一個來源ID對一tenant | 同名/同聯絡方式是否為同一自然人 |
| Owner移轉與歷史owner | Identity/Property | property只有現行owner關聯 | 移轉生效日、歷史查詢與報表顯示 |
| `permission_overrides` 正式語意 | Identity | 欄位已存在，授權主要使用role/property scope | allow/deny優先序、可覆寫範圍與稽核 |
| Attachment retention、ID影本與hard delete | shared | runtime支援upload/register/read | 保存年限、合法目的、刪除與稽核 |
| Legacy attachment object mapping | migration/shared | legacy metadata存在，object尚無stage | resource對應、缺檔、checksum與挑選範圍 |
| Production cutover write freeze/delta/rollback | migration/operations | importer是one-shot，尚無正式cutover contract | freeze窗口、delta來源、rollback點與責任人 |

## Decision Gate

每一項在移入 canonical rule 前，至少要確認生命週期、授權、歷史資料、刪除/更正、transaction boundary與cross-module side effects。確認結果只寫最終規則；討論過程與方案比較留在對應 ADR 或使用者指定的工作項。
