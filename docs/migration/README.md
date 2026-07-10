# Legacy Migration Documentation

本目錄只保存可版本控制、可審查且不含production data的migration contract。既有`docs/mirgations/`（歷史拼字）是ignored development/UAT input；`artifacts/`是ignored reports/dumps。兩者都不是canonical documentation，也不得force-add。

## Scope And Authority

- [`legacy-estate-scope.md`](legacy-estate-scope.md)：現行importer scope、mapping狀態與known gaps。
- [`accepted-losses.md`](accepted-losses.md)：只收使用者已核准的loss reason；目前production registry為空。
- [`validation-contract.md`](validation-contract.md)：production migration必須滿足的完整性等式與sign-off gates。
- [`run-manifest-contract.md`](run-manifest-contract.md)：private immutable evidence bundle的metadata contract。
- [`production-cutover.md`](production-cutover.md)：尚待確認的freeze/delta/rollback框架。

DB migrations定義新系統schema；本目錄定義legacy data如何被解釋、驗證與切換。Importer implementation只能證明as-is，不能自動建立legacy accounting或business truth。

## Data Classification

| Artifact | Storage | Git policy |
| --- | --- | --- |
| Mapping/validation/cutover rules | `docs/migration/**` | tracked，禁止PII與runtime identifiers |
| Development/UAT JSON fixtures | `docs/mirgations/**` | ignored/private；freshness不是缺陷 |
| Stage/validation reports | `artifacts/legacy_migration/**` | ignored/private |
| SQL dumps | `artifacts/db_dumps/**` | ignored/private，可能含application users與tenant data |
| Production source export/object manifest | private immutable run directory | 不進repo；只在manifest記checksum/count/classification |
| Attachment objects | private source/target storage | 不進repo；只保存必要checksum/size/result evidence |

## Fixture And Production Evidence Boundary

Development fixtures可以固定在某次export以支援rerun與UAT，無須追隨live runtime。Production rehearsal/cutover則必須使用當次final-format export、immutable manifest與完整validation evidence；不得把舊UAT report當作production sign-off。
