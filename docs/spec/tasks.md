# 實作任務清單

產出依據：docs/design/domain-model.md v3.0

---

## 文件目的

本文件分成兩個用途：

1. 作為整體專案 roadmap，保留 schema / domain / API / scheduler 的依賴關係。
2. 作為日常實作指引，提供一套可重複套用的垂直切片流程。

本專案採用務實版 DDD，而不是教科書式 full DDD。

- Read API：不走 Aggregate，直接查詢 SQL / GORM 組合 DTO。
- Write API：走 Application Service + Aggregate + Repository。
- OpenAPI schema 服務 HTTP contract，不直接當 domain model。
- Aggregate 服務 business rule，不直接當 API response model。
- Migration、schema、index、constraint 採 SQL-first，不以 ORM migration 為主。
- GORM 可作為 persistence helper，但不作為架構中心；複雜查詢、報表、scheduler scan、locking query 可直接使用 raw SQL。

### Data Access 分層

- `model`：大致對應 table，負責 persistence mapping。
- `repository`：對應 use case / aggregate / bounded context，只提供當前業務切片真正需要的方法。
- `query`：服務 read model、報表、scheduler scan，直接回 DTO 或輕量 query object。

補充原則：

- 不採「每個 table 先建完整 CRUD repo」策略。
- persistence model、domain model、API model 不混用。
- Read 與 scheduler 查詢可直接走 query repository，不強迫經過 aggregate repository。

---

## 目前開發策略

### 目標

建立一套可以持續複用的單支 API 實作流程，而不是先把整個 BC 一次鋪完。

### 實作原則

- 以單支 API / 單個 use case 為最小工作單位。
- 優先完成一條從 handler 到 DB 的完整路徑，再複製到下一支 API。
- Read 與 Write 分開思考，不強迫所有 API 都先經過 Aggregate。
- Repository 只實作當前 use case 需要的方法，不先做全表 CRUD。
- 排程採外部 scheduler 呼叫 backend endpoint；排程邏輯仍放在本 repo 的 application / repository 層。
- 先求固定套路，再求抽象完整度。

### 測試策略

本專案目前不採 TDD。

- 預設流程為：先實作，再補測試。
- 不要求在介面未穩定前先寫大量 stub / mock。
- 單人開發優先避免為測試而測試，重點放在關鍵規則與核心流程驗證。

建議測試優先順序：

1. 寫完 API 後，先補最小 happy path 測試。
2. 再補最重要的 business rule / error path 測試。
3. 最後再補跨 BC 或整體流程的整合測試。

建議測試重點：

- Read API：handler test、query repository test、必要時整合測試。
- Write API：aggregate rule test、application service test、必要時 handler test。
- 排程 / 跨 BC 流程：以整合測試為主，不追求每層都 mock。

---

## 單支 API 垂直切片流程

### A. Read API 流程

適用：純查詢，不改變狀態，不執行核心 business rule。

1. 確認 API response 與 filter 條件。
2. 定義 query DTO / response mapping。
3. 實作 query repository，直接用 SQL / GORM 查詢。
4. 接回 handler 與 router。
5. 完成後補 handler test / repo test。

常見例子：

- `GET /users/me`
- `GET /properties`
- `GET /properties/{id}`
- `GET /bills`
- `GET /properties/{id}/dashboard`

### B. Write API 流程

適用：建立、修改、狀態轉換、需要套用 business rule。

1. 確認 use case、角色限制、business rule 與 error code。
2. 定義 application input / output。
3. 定義或補齊 Aggregate 與必要 method。
4. 只實作本 use case 需要的 repository 方法。
5. 在 application service 中編排 transaction、aggregate 操作、event 發送。
6. 接回 handler 與 router。
7. 完成後補 aggregate rule test、application service test、必要的 handler test。

常見例子：

- `POST /properties`
- `POST /leases`
- `POST /bills/{id}/meter`
- `POST /bills/{id}/payment`
- `POST /leases/{id}/terminate`

### C. Scheduler Job 流程

適用：由外部 scheduler 定時呼叫的批次任務。

1. 確認 job 觸發頻率、操作範圍、角色/驗證方式。
2. 定義 job endpoint request / response 與 idempotency 規則。
3. 在 application service 中編排批次流程、locking、逐筆失敗策略。
4. 將掃描與批次查詢實作在 query repository 或 job repository。
5. 必要時於 transaction commit 後發送 domain event。
6. 完成後補整合測試或最小 job handler test。

常見例子：

- 逾期帳單掃描
- 租約到期掃描
- 月結快照
- 強制終止補償

---

## 建議開發順序

以下順序用來建立可重複的開發模板，不代表要完整做完一整個 BC 才能往下。

### 第一階段：建立 Query Slice 模板

1. `GET /users/me`
2. `GET /properties/{id}`
3. `GET /properties`

目標：

- 建立 handler -> query repository -> response 的固定套路
- 確認 auth / property access middleware 與 read model 的整合方式

### 第二階段：建立 Command Slice 模板

1. `POST /properties`
2. `POST /properties/{id}/rooms`

目標：

- 建立 handler -> application service -> repository 的固定套路
- 開始區分 transport model、application input、domain model

### 第三階段：進入真正有規則的 Aggregate

1. `POST /leases`
2. `POST /bills/{id}/meter`
3. `POST /bills/{id}/payment`
4. `POST /leases/{id}/terminate`

目標：

- 實際落地 Aggregate + business rule + event flow
- 建立日後複製到 Leasing / Billing / Journal 的核心模板

---

## 依賴關係圖（Roadmap）

```
T-01（基礎建設）
  └── T-02（Identity Schema）
        └── T-03（Property Schema）
              ├── T-04（Leasing Schema）
              │     └── T-05（Billing Schema）
              │           └── T-06（PropertyAccount Schema）
              ├── T-07（Journal Schema）
              └── T-08（強制終止 Schema）

T-06 ──────── T-09（Identity Domain 層）
T-02 ──────── T-10（Property Domain 層）
T-03, T-04 ── T-11（Leasing Domain 層）
T-05, T-06 ── T-12（Billing Domain 層）
T-07 ──────── T-13（Journal Domain 層）

T-09 ── T-14（Identity API 層）
T-10 ── T-15（Property API 層）
T-11 ── T-16（Leasing API 層）
T-12 ── T-17（Billing API 層）
T-13 ── T-18（Journal API 層）

T-09~T-13 ── T-19（排程任務）
T-14~T-19 ── T-20（整合測試）
```

---

## Tasks（Roadmap）

### [T-01] 基礎建設：專案結構、DB migration 工具與 API Runtime Middleware

- **依賴**: 無
- **輸入**: 無
- **產出**: 可執行的 migration 工具、資料庫連線設定、軟刪除 filter 機制、Firebase Admin SDK 整合、Gin middleware 鏈（request ID、logging、auth、authorization、error handling、recovery）、data access 分層原則、排程 endpoint 執行骨架
- **完成條件**:
  - [x] migration 工具可執行 up/down
  - [x] 資料庫連線可正常建立並通過健康檢查
  - [x] 所有查詢可自動加上 `deleted_at IS NULL` filter（middleware 或 ORM scope）
  - [x] `gen_random_uuid()` 可用（PostgreSQL uuid-ossp 或 pgcrypto extension 啟用）
  - [x] Firebase Admin SDK 初始化成功，可驗證 Firebase ID token
  - [x] Auth middleware 完成：驗證 Firebase ID token、解析 Custom Claims、載入對應 DB user，並將 `user_id`、`firebase_uid`、`role`、`assigned_property_ids` 注入 request context
  - [x] Authorization middleware 拆分完成：RBAC（`x-required-role`）與 property ownership（`x-ownership-required`）分離實作，先驗 role 再驗 property access
  - [x] Property ownership 檢查以 Firebase Custom Claims 的 `assigned_property_ids` 為主；設計上保留必要時 fallback DB 查詢的擴充點
  - [x] 統一 error handling middleware 完成：handler / application service 回傳 domain/app error 時，可統一轉為 OpenAPI `ErrorResponse` 格式（`error_code`、`message`、`details`）
  - [x] `401` 與 `403` 錯誤可分別穩定回傳 `UNAUTHORIZED` 與 `FORBIDDEN` 類型 error code；`500` 由 recovery middleware 處理，回應內 `details` 含 `request_id`
  - [x] Request logging middleware 完成：使用 structured logging，至少記錄 `request_id`、`method`、`path`、`status`、`latency_ms`、`user_id`、`firebase_uid`、`role`、`property_id`、`error_code`
  - [x] Logging 不記錄 Authorization header，且不完整記錄 request body；validation / domain error 僅記錄必要摘要
  - [x] Middleware 順序固定並有測試覆蓋：`RequestID -> Recovery -> Logging -> Auth -> RoleAuthorization -> PropertyAuthorization -> Handler`
  - [x] Data access 邊界明確：`model / repository / query` 分層與 SQL-first + GORM 使用邊界已固定
  - [x] Resource ownership resolver 基礎到位：可逐步補上 room / lease / bill / journal / repair -> property_id 查詢能力
  - [x] 排程執行框架到位：外部 scheduler 可呼叫受保護 endpoint，job handler 可進入 application service
  - [x] 排程共通策略固定：idempotency、locking、失敗重跑與 logging 格式有一致做法


---

### [T-02] Schema：users 表

- **依賴**: T-01
- **輸入**: T-01 migration 工具就緒
- **產出**: `users` 表 DDL，含 index
- **完成條件**:
  - [x] `users` 表建立成功，含 `id, firebase_uid, email, name, role, permission_overrides, assigned_property_ids, deleted_at, created_at, updated_at, version` 欄位（無 `password_hash`，v3.0 已移除）
  - [x] `role` CHECK 約束生效（只允許 admin/organizer/staff/owner）
  - [x] `firebase_uid` UNIQUE index 建立（`idx_users_firebase_uid`），軟刪除記錄不受 unique 限制
  - [x] `idx_users_email` unique index 建立，軟刪除記錄不受 unique 限制
  - [x] `idx_users_role` index 建立

---

### [T-03] Schema：properties 與 rooms 表

- **依賴**: T-02
- **輸入**: T-02 users 表就緒（properties.owner_id FK）
- **產出**: `properties`、`rooms` 表 DDL，含 index
- **完成條件**:
  - [x] `properties` 表建立成功，含 `owner_id FK`、`electricity_unit_price CHECK > 0`、`version` 欄位
  - [x] `rooms` 表建立成功，`status CHECK` 約束限制為 vacant/occupied/maintenance
  - [x] `idx_properties_owner_id`、`idx_rooms_property_id`、`idx_rooms_property_status` index 建立
  - [x] `rooms.property_id` FK 指向 `properties.id`

---

### [T-04] Schema：tenants 與 leases 表

- **依賴**: T-03
- **輸入**: T-03 properties/rooms 表就緒
- **產出**: `tenants`、`leases` 表 DDL，含 index
- **完成條件**:
  - [x] `tenants` 表建立成功，含 `status CHECK`（active/inactive）、`version` 欄位
  - [x] `leases` 表建立成功，含 `deposit_amount CHECK >= 0`、`deposit_status CHECK`、`status CHECK`、`version` 欄位
  - [x] `leases.tenant_id`、`leases.room_id`、`leases.property_id` FK 正確指向
  - [x] `idx_leases_property_status`、`idx_leases_tenant_id`、`idx_leases_room_id`、`idx_leases_end_date_status`、`idx_tenants_status` index 建立

---

### [T-05] Schema：bills 表

- **依賴**: T-04
- **輸入**: T-04 leases/rooms 表就緒
- **產出**: `bills` 表 DDL，含 index
- **完成條件**:
  - [x] `bills` 表建立成功，含所有欄位（type/status CHECK、payment_method CHECK、version）
  - [x] `meter_previous_reading`、`meter_current_reading`、`meter_unit_price`、`meter_recorded_at` 欄位建立（nullable）
  - [x] `overdue_notice_count` 欄位預設值為 0
  - [x] `idx_bills_property_status_due_date`、`idx_bills_lease_id`、`idx_bills_status_due_date`、`idx_bills_property_type_due_date`、`idx_bills_room_type_due_date`、`idx_bills_status_overdue_notice_count` 共 6 個 index 建立

---

### [T-06] Schema：property_accounts、accounting_entries、monthly_snapshots、monthly_snapshot_entries 表

- **依賴**: T-03
- **輸入**: T-03 properties 表就緒
- **產出**: PropertyAccount 相關 4 張表 DDL，含 index
- **完成條件**:
  - [x] `property_accounts` 表建立，`property_id` UNIQUE constraint 生效
  - [x] `accounting_entries` 表建立，`category CHECK` 約束限制 5 種類別，含 `year/month` 欄位
  - [x] `monthly_snapshots` 表建立，`(property_id, year, month)` UNIQUE constraint 生效
  - [x] `monthly_snapshot_entries` 表建立，`snapshot_id FK` 正確
  - [x] `idx_accounting_entries_account_year_month`、`idx_monthly_snapshots_property_year_month`、`idx_monthly_snapshot_entries_snapshot_category` index 建立

---

### [T-07] Schema：journal_logs 與 repair_requests 表

- **依賴**: T-03
- **輸入**: T-03 properties/rooms 表就緒，T-02 users 表就緒
- **產出**: `journal_logs`、`repair_requests` 表 DDL，含 index
- **完成條件**:
  - [x] `journal_logs` 表建立，`author_id FK` 指向 users，`room_id` nullable FK 指向 rooms
  - [x] `repair_requests` 表建立，`status CHECK` 約束限制 5 種狀態，`assigned_to` nullable FK 指向 users
  - [x] `idx_journal_logs_property_created_at`（含 DESC 排序）、`idx_repair_requests_property_status`、`idx_repair_requests_room_id` index 建立

---

### [T-08] Schema：force_terminations 與 force_termination_bills 表

- **依賴**: T-04, T-05
- **輸入**: T-04 leases 表就緒，T-05 bills 表就緒，T-02 users 表就緒
- **產出**: `force_terminations`、`force_termination_bills` 表 DDL，含 index
- **完成條件**:
  - [x] `force_terminations` 表建立，`status CHECK`（in_progress/completed）、`initiated_by FK` 指向 users
  - [x] `force_termination_bills` 表建立，`status CHECK`（pending/done）
  - [x] `idx_force_terminations_status`、`idx_force_termination_bills_ft_status`、`idx_force_termination_bills_bill_id` index 建立

---

### [T-09] Domain 層：Identity & Access（User Aggregate）

- **依賴**: T-02
- **輸入**: users 表就緒
- **產出**: User Aggregate、相關 Business Rules、Application Service、Firebase middleware
- **完成條件**:
  - [ ] Firebase Admin SDK middleware：每個 request 驗證 Firebase ID token，從 token 中讀取 firebase_uid，查詢 users table 取得 User Aggregate
  - [ ] User Aggregate 可建立、更新角色（無密碼欄位，v3.0 認證由 Firebase 管理）
  - [ ] 物業指派（assigned_property_ids）更新後，呼叫 Firebase Admin SDK 更新該使用者的 Custom Claims，`PropertyUnassigned` event 正確發出
  - [ ] BR-11：指派對象為 owner 角色時，Application Service 拋出 `CANNOT_ASSIGN_PROPERTY_TO_OWNER` 錯誤
  - [ ] BR-13：admin 嘗試降低自己角色時，Application Service 拋出 `ADMIN_CANNOT_DOWNGRADE_SELF` 錯誤
  - [ ] 樂觀鎖（version 欄位）衝突時回傳 `CONCURRENT_UPDATE_CONFLICT` 錯誤

---

### [T-10] Domain 層：Property（Property Aggregate）

- **依賴**: T-03
- **輸入**: properties、rooms 表就緒
- **產出**: Property Aggregate、Room 狀態機、相關 Business Rules
- **完成條件**:
  - [ ] Room 狀態機：`vacant → occupied`（訂閱 LeaseCreated）、`occupied → vacant`（訂閱 LeaseTerminated）轉換正確執行
  - [ ] `vacant → maintenance`（RoomSetToMaintenance）正確觸發 event
  - [ ] `maintenance → vacant`（訂閱 RepairCompleted/RepairCancelled）：確認同 room_id 所有 RepairRequest 均 completed 或 cancelled 後才轉換
  - [ ] BR-07：有 `status = occupied` 房間的物業，刪除時拋出 `PROPERTY_HAS_OCCUPIED_ROOMS` 錯誤，回傳 occupied_room_ids
  - [ ] BR-08：`status = occupied` 或 `maintenance` 的房間，刪除時分別拋出 `ROOM_IS_OCCUPIED`、`ROOM_IS_IN_MAINTENANCE` 錯誤
  - [ ] PropertyCreated event 發出後，Billing BC 自動建立 PropertyAccount（訂閱正確執行）

---

### [T-11] Domain 層：Leasing（Tenant Aggregate、Lease Aggregate）

- **依賴**: T-04
- **輸入**: tenants、leases 表就緒
- **產出**: Tenant Aggregate、Lease Aggregate、Business Rules、Application Service
- **完成條件**:
  - [ ] BR-01：start_date > end_date 時拋出 `LEASE_INVALID_DATE_RANGE` 錯誤
  - [ ] BR-02：rent_amount = 0 時拋出 `LEASE_RENT_AMOUNT_ZERO` 錯誤
  - [ ] BR-03：deposit_amount < 0 時拋出 `LEASE_DEPOSIT_NEGATIVE` 錯誤
  - [ ] BR-12：建立租約前對 Room 取悲觀鎖（SELECT FOR UPDATE），若 status 非 vacant 則拋出 `ROOM_NOT_VACANT` 錯誤
  - [ ] LeaseCreated event 發出後，Tenant status 若為 inactive 自動改回 active
  - [ ] BR-04：正常終止時，有未結清帳單（pending_payment/pending_meter/overdue）則拋出 `LEASE_HAS_UNPAID_BILLS` 錯誤，回傳 unpaid_bill_ids
  - [ ] BR-10：押金扣款 action 未填寫 reason 時拋出 `DEPOSIT_DEDUCTION_REASON_REQUIRED` 錯誤
  - [ ] LeaseConditionChanged：void `due_date >= nextPaymentDate` 且 status 為 pending_payment/pending_meter 的帳單，從 nextPaymentDate 重產帳單（BR-15 計算邏輯正確）
  - [ ] LeaseTerminated 後：Tenant status 自動更新（查詢是否還有 active/expired Lease）
  - [ ] Lease 帳單預產：建立租約時，從 start_date 到 end_date 每月依 BR-15 計算付款日，預產租金帳單（pending_payment）與電費帳單（pending_meter）

---

### [T-12] Domain 層：Billing（Bill Aggregate、PropertyAccount Aggregate）

- **依賴**: T-05, T-06
- **輸入**: bills、property_accounts、accounting_entries 表就緒
- **產出**: Bill Aggregate、PropertyAccount Aggregate、Business Rules、排程掃描邏輯
- **完成條件**:
  - [ ] BR-05：帳單 status = paid 時，收款操作拋出 `BILL_ALREADY_PAID` 錯誤
  - [ ] BR-06：電表抄錄 current_reading < previous_reading 時，拋出 `METER_READING_LESS_THAN_PREVIOUS` 錯誤（回傳 previous_reading 值）
  - [ ] BR-16：MeterRecorded 時，系統計算 amount = usage × unitPrice，不接受員工直接傳入 amount 欄位
  - [ ] BillPaid event 發出後，PropertyAccount 新增正確 category 的 AccountingEntry
  - [ ] LeaseTerminated event 訂閱：void 該 lease_id 所有 pending_payment 和 pending_meter 帳單
  - [ ] 樂觀鎖衝突（逾期掃描 vs 付款）：Application Service 正確處理，付款方收到 `CONCURRENT_UPDATE_CONFLICT`，批次任務跳過
  - [ ] 強制終止流程：ForceTermination 記錄建立，逐一將 bills 標記 written_off 並更新 force_termination_bills.status = done

---

### [T-13] Domain 層：Journal（JournalLog Aggregate、RepairRequest Aggregate）

- **依賴**: T-07
- **輸入**: journal_logs、repair_requests 表就緒
- **產出**: JournalLog Aggregate、RepairRequest Aggregate、狀態機
- **完成條件**:
  - [ ] JournalLog 建立時，若有 expense_amount，正確發出 `JournalExpenseRecorded` event
  - [ ] RepairRequest 狀態機：submitted→assigned→in_progress→completed 轉換正確；submitted/assigned/in_progress→cancelled 正確
  - [ ] 無效狀態轉換（如 completed → assigned）時，分別拋出 `REPAIR_INVALID_STATUS_FOR_ASSIGN`、`REPAIR_INVALID_STATUS_FOR_PROGRESS`、`REPAIR_INVALID_STATUS_FOR_COMPLETE`、`REPAIR_ALREADY_COMPLETED` 錯誤
  - [ ] RepairCompleted / RepairCancelled event 正確發出，包含 roomId 與 propertyId
  - [ ] Property BC 訂閱 RepairCompleted/RepairCancelled：查詢同 room_id 所有非軟刪除 RepairRequest，確認全為 completed 或 cancelled 後才將 Room status 改為 vacant

---

### [T-14] API 層：Identity & Access endpoints

- **依賴**: T-09
- **輸入**: T-09 User Aggregate、Application Service、Firebase middleware 就緒
- **產出**: POST /auth/sync、GET/POST /users、GET /users/me、GET/PATCH /users/{id}、POST /users/{id}/property-assignments
- **完成條件**:
  - [ ] POST /auth/sync：Firebase ID token 有效時回傳使用者資料（id, firebase_uid, role, assigned_property_ids）；token 無效時回傳 401 `INVALID_FIREBASE_TOKEN`；firebase_uid 在 DB 不存在時回傳 404 `USER_NOT_FOUND`
  - [ ] POST /users：email 重複時回傳 409 `EMAIL_ALREADY_EXISTS`；staff 嘗試建立成員帳號時回傳 403 `FORBIDDEN`；建立成功後由後端向 Firebase 產生 password reset link，並透過 Resend 寄送設定密碼信
  - [ ] PATCH /users/{id}：admin 降低自己角色時回傳 422 `ADMIN_CANNOT_DOWNGRADE_SELF`；不含 password 欄位（密碼由 Firebase 管理）
  - [ ] POST /users/{id}/property-assignments：指派成功後呼叫 Firebase Admin SDK 更新 Custom Claims；指派給 owner 角色時回傳 422 `CANNOT_ASSIGN_PROPERTY_TO_OWNER`

---

### [T-15] API 層：Property endpoints

- **依賴**: T-10
- **輸入**: T-10 Property Aggregate、Domain Service 就緒
- **產出**: CRUD for properties/rooms、POST /rooms/{id}/maintenance、GET /properties/{id}/dashboard
- **完成條件**:
  - [ ] PATCH /properties/{id}：staff 修改 electricity_unit_price 時回傳 403 `FORBIDDEN_ELECTRICITY_PRICE_UPDATE`
  - [ ] DELETE /properties/{id}：有 occupied 房間時回傳 422 `PROPERTY_HAS_OCCUPIED_ROOMS`，response 包含 occupied_room_ids
  - [ ] DELETE /rooms/{id}：occupied 房間回傳 422 `ROOM_IS_OCCUPIED`；maintenance 房間回傳 422 `ROOM_IS_IN_MAINTENANCE`
  - [ ] POST /rooms/{id}/maintenance：成功後 room status 為 maintenance，RoomSetToMaintenance event 已發出
  - [ ] GET /properties/{id}/dashboard：回傳包含房間狀態、本月收支摘要、逾期帳單數、最近 5 筆 Journal 的完整資料

---

### [T-16] API 層：Leasing endpoints

- **依賴**: T-11
- **輸入**: T-11 Tenant/Lease Aggregate、Application Service 就緒
- **產出**: CRUD for tenants/leases、POST /leases/{id}/terminate、POST /leases/{id}/force-terminate、PATCH /leases/{id}/deposit
- **完成條件**:
  - [ ] POST /leases：BR-01/BR-02/BR-03/BR-12 各自回傳對應的 422 error code；成功後 LeaseCreated event 發出，帳單已預產
  - [ ] PATCH /leases/{id}（租金調整）：staff 操作回傳 403；BR-02 回傳 422 `LEASE_RENT_AMOUNT_ZERO`；成功後 LeaseConditionChanged event 發出
  - [ ] POST /leases/{id}/terminate：有未清帳單回傳 422 `LEASE_HAS_UNPAID_BILLS`（含 unpaid_bill_ids）；押金扣款無原因回傳 422 `DEPOSIT_DEDUCTION_REASON_REQUIRED`；成功後 LeaseTerminated（forced:false）event 發出
  - [ ] POST /leases/{id}/force-terminate：staff 操作回傳 403 `FORBIDDEN_FORCE_TERMINATION`；未填原因回傳 422 `FORCE_TERMINATION_REASON_REQUIRED`；成功後回傳 202，force_termination 記錄已建立

---

### [T-17] API 層：Billing endpoints

- **依賴**: T-12
- **輸入**: T-12 Bill Aggregate、PropertyAccount Aggregate 就緒
- **產出**: GET /bills, GET /bills/{id}、POST /bills/{id}/meter、POST /bills/{id}/payment、財報 endpoints
- **完成條件**:
  - [ ] POST /bills/{id}/meter：BR-06 回傳 422 `METER_READING_LESS_THAN_PREVIOUS`（含 previous_reading 詳情）；成功後帳單 amount 計算正確（usage × unitPrice），status 從 pending_meter 改為 pending_payment
  - [ ] POST /bills/{id}/payment：BR-05 回傳 422 `BILL_ALREADY_PAID`；overdue 帳單可正常收款（回傳 200，status 改為 paid）；成功後 BillPaid event 發出
  - [ ] GET /bills/{id}：電費帳單回傳時自動帶入 previous_reading（查詢同 room_id 最近一張有 meter_reading 的帳單）
  - [ ] POST /properties/{id}/financial-report/{year}/{month}/send：staff 操作回傳 403；成功後財報 Email 寄送

---

### [T-18] API 層：Journal endpoints

- **依賴**: T-13
- **輸入**: T-13 JournalLog/RepairRequest Aggregate 就緒
- **產出**: CRUD for journal-logs/repair-requests、維修狀態機 action endpoints
- **完成條件**:
  - [ ] POST /journal-logs：含 expense_amount 時，JournalExpenseRecorded event 發出，PropertyAccount 的 accounting_entries 新增一筆 journal_expense 記錄
  - [ ] POST /repair-requests/{id}/assign：非 submitted 狀態回傳 422 `REPAIR_INVALID_STATUS_FOR_ASSIGN`
  - [ ] POST /repair-requests/{id}/complete：成功後 RepairCompleted event 發出；若該 Room 所有 RepairRequest 均 completed/cancelled，Room status 自動改為 vacant
  - [ ] POST /repair-requests/{id}/cancel：completed 狀態回傳 422 `REPAIR_ALREADY_COMPLETED`；成功後 RepairCancelled event 發出

---

### [T-19] 排程任務實作

- **依賴**: T-11, T-12
- **輸入**: leases、bills、force_terminations 表與 Domain Service 就緒
- **產出**: 6 個排程任務（由外部 scheduler 呼叫 backend endpoint 觸發）
- **完成條件**:
  - [ ] 逾期帳單掃描（每日凌晨）：掃描 `due_date < today AND status = pending_payment AND deleted_at IS NULL`，樂觀鎖衝突時跳過該筆帳單（不拋錯）
  - [ ] 逾期催收通知（每週）：掃描 `status = overdue AND overdue_notice_count < 3 AND deleted_at IS NULL`，寄送 Email 後 overdue_notice_count + 1；超過 3 次的帳單不再寄送
  - [ ] 租約到期掃描（每日凌晨）：掃描 `end_date < today AND status = active AND deleted_at IS NULL`，批次更新 status 為 expired，Room status 維持 occupied
  - [ ] 租約到期提醒（每日凌晨）：掃描 `end_date = today + 30 days AND status = active`，寄送提醒 Email 給主辦和員工（role IN (organizer, staff)）
  - [ ] 月結快照（每月最後一天 23:59）：將 PropertyAccount 當月 AccountingEntry 封存至 monthly_snapshots + monthly_snapshot_entries，清空 accounting_entries，排除 `deleted_at IS NOT NULL` 的物業
  - [ ] 強制終止補償（每日凌晨）：掃描 `force_terminations WHERE status = in_progress`，查詢 force_termination_bills WHERE status = pending，逐一執行 written_off，完成後更新 bill.status = written_off 並更新 force_termination_bills.status = done；所有 bill 完成後 force_terminations.status 改為 completed，發出 LeaseTerminated（forced:true）event
  - [x] 所有 job 皆透過受保護 endpoint 觸發，不依賴 app 內建 cron 或 CLI job
  - [x] 所有 job 皆具 idempotency 與重跑安全性；同一個排程視窗重送不造成重複副作用

---

### [T-20] 整合測試

- **依賴**: T-14, T-15, T-16, T-17, T-18, T-19
- **輸入**: 所有 API 與 Domain 層實作完成
- **產出**: 端到端關鍵路徑測試
- **完成條件**:
  - [ ] 完整租約生命週期：建立 Tenant → 建立 Lease（帳單預產驗證）→ 電表抄錄 → 收款 → 正常終止（LeaseTerminated event 觸發 Room vacant + 通知 Email）
  - [ ] 強制終止路徑：建立 Lease → 模擬逾期帳單 → 觸發強制終止 → 補償排程執行 → 所有帳單 written_off → LeaseTerminated（forced:true）發出
  - [ ] 跨 BC event 傳遞正確：LeaseCreated → Room 改 occupied；RepairCompleted → Room 改 vacant（所有 RepairRequest 完成後才轉換）
  - [ ] 樂觀鎖測試：兩個請求同時對同一帳單收款，只有一個成功，另一個回傳 409 `CONCURRENT_UPDATE_CONFLICT`
  - [ ] Resource-based 存取控制：organizer 只能存取 Firebase Custom Claims 內 assigned_property_ids 的資源；嘗試存取未指派物業時回傳 403 `FORBIDDEN`
  - [ ] Firebase Auth 整合：Firebase ID token 驗證正確，Custom Claims 更新（物業指派變更後）於下次 token refresh 生效
