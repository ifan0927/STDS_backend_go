# Error Code 規劃

產出日期：2026-04-15

## 目標

先定義 Go 層共用 error 型別與常見 cross-cutting error code，避免後續各 handler / service 各自散寫字串。

本文件**不要求一次產出所有 BC 專屬錯誤**。業務錯誤在各 BC 實作時，依 OpenAPI 與 task spec 補入各自模組。

---

## Go 目錄約定

### 共用 error 基礎設施

- 位置：`internal/shared/apperr`
- 職責：
  - 定義共用 error 型別 `apperr.Error`
  - 定義跨 BC 共用的 error code
  - 提供 middleware / handler 可共用的 error contract

### BC 專屬錯誤

- 原則：放在**最接近規則所有權**的 package
- 建議位置：
  - domain business rule error：`internal/domain/<bc>/errors.go`
  - application/use case error：`internal/application/<bc>/errors.go`
- 不要把所有 BC error 都集中塞進 `internal/shared/apperr`

---

## 共用 error contract

Go 層共用型別：

- `Code`：穩定 error code，對應 OpenAPI `error_code`
- `Message`：預設訊息
- `HTTPStatus`：HTTP status mapping
- `Details`：可選附加資訊
- `Cause`：底層 wrapped error

這層先作為 middleware、handler、application service 的共同語言。

---

## 第一批共用 error code

以下先放 cross-cutting / infrastructure 常見錯誤：

| Code | HTTP Status | 用途 |
|------|-------------|------|
| `INTERNAL_SERVER_ERROR` | `500` | 未知錯誤或未映射錯誤的最終 fallback |
| `UNAUTHORIZED` | `401` | 缺少憑證或未通過認證 |
| `INVALID_FIREBASE_TOKEN` | `401` | Firebase token 無效、過期、格式錯誤 |
| `FORBIDDEN` | `403` | 已認證但無權限 |
| `CONCURRENT_UPDATE_CONFLICT` | `409` | 樂觀鎖或併發更新衝突 |

---

## 後續擴充規則

## Billing error code decisions

Issue #18 實作帳單讀取、抄表與收款時，Billing BC 需新增下列業務錯誤：

| Code | HTTP Status | 用途 |
|------|-------------|------|
| `BILL_ALREADY_PAID` | `422` | 帳單已收款，拒絕重複收款 |
| `BILL_STATUS_NOT_PAYABLE` | `422` | 帳單狀態不是 `pending_payment` 或 `overdue`，不可收款 |
| `BILL_PAID_AMOUNT_MISMATCH` | `422` | `paid_amount` 不等於帳單 `amount`；不支援部分收款或溢收 |
| `METER_READING_LESS_THAN_PREVIOUS` | `422` | 抄表讀數小於上一個已完成 billing period 的讀數 |
| `BILL_NOT_ELECTRICITY_TYPE` | `422` | 非電費帳單不可抄表 |
| `BILL_STATUS_NOT_RECORDABLE` | `422` | 電費帳單狀態不是 `pending_meter`，不可抄表 |

### 可以先放進 shared 的錯誤

- auth / middleware / transport 層共用
- 多個 BC 都會重複使用
- 不依賴特定 aggregate 的 business vocabulary

### 不要先放進 shared 的錯誤

- `LEASE_HAS_UNPAID_BILLS`
- `ROOM_NOT_VACANT`
- `PROPERTY_HAS_OCCUPIED_ROOMS`
- `BILL_ALREADY_PAID`

這類錯誤應留在各 BC 實作時，依 spec 寫入對應模組。

---

## 實作順序

1. 先使用 `internal/shared/apperr` 作為共用基礎
2. 建立 HTTP error mapping middleware / helper
3. 各 BC 開始實作時，再新增各自的 `errors.go`
4. 新增 BC error 時，同步對齊 OpenAPI `error_code` 與 task 驗收條件
