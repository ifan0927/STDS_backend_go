# Production Cutover Contract

- Status: Needs decision
- Production use: blocked until every TBD below is confirmed and rehearsed

本文只列出必須決定與驗證的cutover邊界，不替產品或operator選答案。Staging/UAT dump/import流程不能直接升格為production contract。

## Required Decisions

| Decision | Status | Required output |
| --- | --- | --- |
| Legacy write freeze owner、start/end與例外 | TBD | 可執行freeze checklist與責任人 |
| Freeze前後delta capture來源與ordering | TBD | Delta format、dedupe/idempotency與final watermark |
| Final export與immutable manifest交付 | TBD | Export command、checksum/count與custody evidence |
| Go/no-go authority與blocking gate處理 | TBD | Sign-off matrix與abort conditions |
| Runtime traffic switch順序 | TBD | Rehearsed sequence與read/write smoke |
| Rollback point與data divergence處理 | TBD | Restore/roll-forward criteria與maximum window |
| RTO / RPO | TBD | Approved objectives與measured rehearsal evidence |
| Attachment/object cutover | TBD | Copy/freeze/delta/verification order |

## Minimum Rehearsal

1. 使用當次final-format export與private immutable manifest。
2. 在空白、隔離的PostgreSQL執行完整schema migrations與所有legacy stages。
3. 執行full rerun，證明idempotency與locking/uniqueness behavior。
4. 通過[`validation-contract.md`](validation-contract.md)所有blocking gates。
5. 執行authorized API/UI spot checks與finance/access/attachment sign-off。
6. 演練abort/rollback並量測實際時間與data loss boundary。
7. 保存private evidence bundle，不把dump/report/PII加入Git或PR。

在上述decision與rehearsal完成前，production migration readiness維持unresolved。
