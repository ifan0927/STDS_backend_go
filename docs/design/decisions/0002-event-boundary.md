# ADR 0002: Transaction And Post-Commit Event Boundary

- Status: Accepted with known risk
- Last verified: 2026-07-10

## Context

現行event bus在DB commit後同步dispatch，沒有outbox、retry或durable delivery。部分核心room/tenant lifecycle side effects仍使用這條路徑。

## Decision

需要與command保持強一致的核心狀態與財務side effects，必須在application command的同一DB transaction內direct orchestration。Post-commit subscriber只適用於失敗後不需rollback原command、且沒有durable delivery需求的follow-up。Published-only event不得被文件描述成已實作downstream behavior。

## Consequences

- Payment/accounting、deposit、checkout、journal expense、property account與maintenance/repair creation維持同transaction。
- Lease與room/tenant status subscribers是current as-is exception與已知風險，不代表此模式適合新增核心狀態。
- 若要消除exception，需另行決定direct orchestration、reconciliation或durable delivery方案；本文件不改application behavior。
