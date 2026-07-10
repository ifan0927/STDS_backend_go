# ADR 0001: DB Principal Is The Authorization Source

- Status: Accepted
- Last verified: 2026-07-10

## Context

Firebase ID token能證明外部身份，repo同時保存role、property assignments與permission metadata。舊文件曾把Custom Claims描述為runtime authorization authority，會造成claims與DB drift時的雙重真相。

## Decision

Firebase Auth只負責驗證身份。Backend以verified `firebase_uid`載入active DB user，並以DB principal的role與assigned properties執行RBAC/resource authorization。Owner access另以property ownership解析。Custom Claims可以作同步或相容資料，但不得覆蓋DB principal。

## Consequences

- 每個authenticated request需要DB principal lookup。
- User不存在、停用或DB assignment更新時，以DB現況為準。
- `permission_overrides`的business semantics未由本ADR決定，仍在decision backlog。
