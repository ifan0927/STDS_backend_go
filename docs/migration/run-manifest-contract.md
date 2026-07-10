# Private Run Manifest Contract

每次production rehearsal/cutover使用一個private immutable run directory。Repo只版本控制本contract，不保存實際manifest、source data、dump、credentials、PII、完整object URLs或runtime reports。

## Required Metadata

- Run ID與purpose：rehearsal或cutover。
- Source system identifier、source schema/version、export timestamp與timezone。
- Source code/version或bundle checksum；不得包含credential。
- 每個source file/object的logical name、SHA-256、byte size、row count與PII classification。
- Export query/tool version與target application commit/tool version。
- Target DB schema migration version。
- Stage execution order、start/end time、exit status與report checksum。
- Mapping/validation contract version。
- Approved loss reason codes與核准reference；敏感細節留private evidence。
- Operator/reviewer role identifiers與sign-off timestamps；避免不必要個資。
- Artifact retention/expiry與storage access policy。

## Immutability And Security

- Manifest寫入後以checksum與read-only/immutable storage policy保護。
- Secret、token、database URL、OAuth JSON、signed URL與raw PII不可寫入manifest或log。
- Report失敗但DB已commit時，run必須標記`evidence-incomplete`並停止promotion；不得重跑後覆蓋原run directory。
- Correction建立新run或versioned addendum，不修改既有evidence。
