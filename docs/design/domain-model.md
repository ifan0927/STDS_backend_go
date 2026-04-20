Domain Model

> 版本：v3.3
> 更新說明：
> - v2.0：經四輪多角色設計評審產出
> - v2.1-v2.8：歷次 Validation 修正
> - v2.9：補 paymentMethod 欄位、expired → terminated 狀態轉換、Tenant status 轉換邏輯、monthly_snapshots 拆兩層 table、force_termination_bills 拆表移除 bill_ids[]、補 overdue_notice_count Index、Notification event payload 要求
> - v3.0：認證機制改為 Firebase Auth + Custom Claims，移除自建 JWT 與 password_hash，users table 改存 firebase_uid
> - v3.2：新增共用附件機制（GCS Signed URL + nonce 綁定 + 各資源獨立附件表），補 BR-18、附件相關 Read Models、排程任務、ADR
> - v3.3：電費單價改為可接受浮點數；電費帳單 amount 維持台幣整數，計算後採四捨五入

---

## Bounded Contexts

| BC 名稱 | 核心職責 | 上游依賴 | 下游消費者 |
|---------|---------|---------|-----------|
| Identity & Access | 使用者帳號、角色、權限管理、物業指派 | 無 | 所有 BC |
| Property | 物業與房間基本資料及狀態管理 | Identity & Access | Leasing, Journal, Billing |
| Leasing | 租客資料、租約建立／終止、帳單預產 | Property, Identity & Access | Billing, Property, Notification |
| Billing | 電表抄錄、費用計算、帳單收款、財報產生 | Leasing, Property | Notification |
| Journal | 物業日誌記錄、維修派工、報修通報、日誌費用 | Property, Identity & Access | Billing, Property, Notification |
| Notification | 各類通知寄送、收據與財報檔案產生與輸出 | 所有 BC | 無 |

> **v2.1 修正**：Billing 上游依賴移除 Journal（Journal 是事件發出方，Billing 是消費者，非 Billing 主動呼叫 Journal）。Journal 下游消費者補上 Billing。
>
> **架構說明**：STDS 為單一服務（monolith），BC 為邏輯邊界，不是部署邊界。跨 BC 的協調透過 in-process event bus 或 Application Service 直接呼叫，無需 message queue。

---

## Bounded Context 說明

### Identity & Access

負責工作室成員與業主的帳號管理，包含角色指派、個別權限覆寫，以及將物業指派給成員。授權模型為混合型：RBAC 做基礎功能控管，物業層級採 resource-based 控管。

業主帳號由工作室建立，業主只能登入查看自己物業狀況，無任何修改權限。

**物業指派為 Identity BC 的唯一來源**，Property BC 及其他 BC 查詢操作權限時，呼叫 Identity BC 的 Application Service 驗證，不另存副本。

**認證機制**：系統使用 Firebase Auth 管理使用者認證。後端不儲存密碼，使用者帳號在 Firebase 建立，DB 的 `users` table 透過 `firebase_uid` 與 Firebase 帳號對應。登入流程由前端 Firebase SDK 處理；建立帳號後的設定密碼信由後端向 Firebase 產生 password reset link，再透過 email 發送。既有使用者若需重寄設定密碼信，由系統管理員透過管理 API 觸發。

### Leasing

管理租客資料與租約生命週期。租客資料（Tenant）與租約（Lease）獨立建立，先建 Tenant 再建 Lease。Tenant record 長期保留，退租後再租直接關聯同一 Tenant，新建 Lease。

**不支援 LeaseRenewed**：續約一律以「舊租約終止 + 新租約建立」處理，確保帳單和押金歷史清晰。

租約建立時，系統於整個租約期間預產所有帳單（租金帳單 + 電費帳單）。租金帳單初始狀態為 `pending_payment`，電費帳單初始狀態為 `pending_meter`，等待每月抄表後更新金額。

**付款日語意**：付款日固定為租約起始日當天，每月同一天。該月無此日期（如 1/31 → 2 月）則順延至該月最後一天。付款日不可單獨修改，屬於租約起始條件的一部分。

租金調整（`LeaseConditionChanged`）時，void 所有 `due_date >= nextPaymentDate` 且狀態為 `pending_payment` 或 `pending_meter` 的帳單（狀態改為 `voided`），從 `nextPaymentDate` 起重產新金額帳單。`nextPaymentDate` 為 operationDate 之後的第一個付款日，計算規則同 BR-15。當月帳單不受影響，即使尚未到期。`LeaseConditionChanged` 僅涵蓋租金金額調整，不含付款日修改。

### Billing

電表抄錄、帳單管理與收款確認。帳單分為租金帳單與電費帳單，電費帳單在租約建立時即預產（`pending_meter`），等待 `MeterRecorded` 事件觸發金額更新後進入 `pending_payment`。

每日排程任務掃描逾期帳單（`due_date < today AND status = pending_payment`），批次更新為 `overdue`。允許 `overdue → paid` 轉換（逾期帳單仍可收款）。

收款確認後產生 `AccountingEntry`，透過 `BillPaid` event 寫入 `PropertyAccount`。

**LeaseTerminated 後的處理**：Billing BC 訂閱 `LeaseTerminated` 後，void 該租約所有剩餘 `pending_payment` 和 `pending_meter` 狀態的帳單（狀態改為 `voided`）。正常終止時帳單應已全清，此步驟主要處理強制終止後尚未 written_off 的預產帳單。

**強制終止租約**（呆帳情境）：由主辦以上角色執行，流程記錄於 `force_terminations` table（Billing BC），未結清帳單標記為 `written_off`，強制發出 `LeaseTerminated`。

### Journal

分為兩個子模組：

- **JournalLog**：純文字紀錄與費用記錄（如日常維護費用），無狀態機
- **RepairRequest**：報修派工，有完整狀態機，派工對象為系統內員工（員工負責線下聯絡廠商）

### Notification

租客無系統帳號，所有通知以 Email 發送。通知發送紀錄不存儲，發出即視為完成。

**通知觸發時機：**

| 通知類型 | 觸發時機 | 收件人 | 觸發來源 |
|---------|---------|-------|---------|
| 帳單通知 | 帳單預產完成（`RentBillsGenerated`） | 租客 | Event（payload 需含：tenantEmail, tenantName, roomName, bills[]{amount, dueDate, type}） |
| 收款收據 | 帳單收款確認（`BillPaid`） | 租客 | Event（payload 需含：tenantEmail, tenantName, roomName, amount, paidAt, billType） |
| 逾期催收通知 | 每週排程，最多 3 次 | 租客 | 排程任務 |
| 退租確認 | `LeaseTerminated`（forced: false **且** isRenewal: false） | 租客 | Event（payload 需含：tenantEmail, tenantName, roomName） |
| 強制終止通知 | `LeaseTerminated`（forced: true） | 租客 | Event（payload 需含：tenantEmail, tenantName, roomName） |
| 租約到期提醒 | 到期前 30 天排程 | 主辦、員工 | 排程任務 |
| 財報寄送 | 主辦手動審核後觸發 | 業主 | 人工操作 |

---

## Aggregates

### Tenant Aggregate（Leasing BC）

- **Root Entity**：Tenant
- **包含**：基本資料、聯絡方式清單（Value Objects）
- **status**：`active | inactive`（有效租約時 `active`，所有租約終止後改 `inactive`）
- **status 轉換邏輯**：Leasing BC Application Service 在 `LeaseTerminated` 後，查詢該 Tenant 是否還有其他 `active` 或 `expired` 的 Lease，若無則自動改為 `inactive`。新建 Lease 時若 Tenant 為 `inactive`，自動改回 `active`。
- **一致性邊界**：租客資料自成一體，與租約獨立建立；Tenant record 長期保留不刪除
- **併發策略**：樂觀鎖

### Lease Aggregate（Leasing BC）

- **Root Entity**：Lease
- **status**：`active | expired | terminated | force_terminated`
  - `active`：建立時預設
  - `expired`：到期日到達，排程標記，房間維持 `occupied` 等人工處理；允許直接執行正常終止（補辦手續），條件同正常終止（帳單全清）
  - `terminated`：正常終止（含從 expired 補辦）
  - `force_terminated`：強制終止
- **包含**：
  - 租約條件（租金、起訖日）
  - `deposit: Deposit`（Value Object）
    - `amount`（原始押金金額）
    - `deductionAmount`（optional，扣款金額）
    - `deductionReason`（optional）
    - `refundAmount`（optional，退還金額）
    - `status: held | settled | written_off`（初始狀態為 `held`，租約建立時自動設定）
      - `settled`：押金處理完畢，涵蓋全額退還（refundAmount = amount）、全額扣款（deductionAmount = amount）、部分扣款後退餘額（deductionAmount + refundAmount = amount）三種情境
      - `written_off`：強制終止時標記
- **一致性邊界**：
  - 帳單隨租約刪除而刪除
  - 正常終止：需所有帳單結清（含押金狀態確認）才能執行
  - 強制終止：主辦以上角色執行，未結清帳單標記 `written_off`，記錄呆帳原因
  - 押金退還／扣款透過 Lease Aggregate 操作
- **併發策略**：樂觀鎖

### Property Aggregate（Property BC）

- **Root Entity**：Property
- **包含**：
  - Room entities（含狀態）
  - `electricityUnitPrice`（台幣正數/度，物業層級電價，可接受小數，例：4.5）
- **Room 狀態**：`vacant | occupied | maintenance`
- **狀態轉換**：
  - `vacant → occupied`：訂閱 `LeaseCreated`
  - `occupied → vacant`：訂閱 `LeaseTerminated`
  - `vacant → maintenance`：主辦或員工手動操作，發出 `RoomSetToMaintenance`
  - `maintenance → vacant`：訂閱 `RepairCompleted` 或 `RepairCancelled`，確認該 Room 所有 RepairRequest 均為 `completed` 或 `cancelled` 後才改回 `vacant`
- **一致性邊界**：房間隨物業刪除而刪除；房間狀態由 Property Aggregate 統一管理
- **併發策略**：樂觀鎖

### Bill Aggregate（Billing BC）

- **Root Entity**：Bill
- **包含**：
  - `type: rent | electricity`（預產時帶入）
  - `room_id`（從 Lease 取得，直接存入，電表查詢不需 JOIN）
  - `lease_id`
  - 付款紀錄（Value Object）
    - `paymentMethod: cash | transfer | other`（收款確認時填入）
    - `paidAt`
    - `paidAmount`
  - 帳單狀態
  - `meterReading: MeterReading`（optional Value Object，電費帳單專用）
    - `previousReading`（系統查詢填入，不由員工輸入）
    - `currentReading`
    - `unitPrice`（抄表當下從 Property 取得並鎖定，台幣正數，可接受小數）
    - `recordedAt`
  - `sourceRef`（optional）：`{ type: 'repair', id: RepairRequestId }`（預留，現階段不實作）
  - `writtenOffReason`（optional，強制終止時使用）
  - `overdue_notice_count`（整數，逾期催收通知已寄次數，上限 3）
- **帳單狀態機**：
  - 租金帳單：`pending_payment → paid | overdue | voided | written_off`
  - 電費帳單：`pending_meter → pending_payment → paid | overdue | voided | written_off`
  - 補充轉換：`overdue → paid`（逾期帳單仍可收款）
- **一致性邊界**：收款確認後產生 `AccountingEntry`，不可重複收款
- **金額儲存**：台幣整數（無小數）
- **電費換算規則**：`rawAmount = usage × MeterReading.unitPrice`，`amount = round(rawAmount)`（四捨五入為最終帳單金額）
- **併發策略**：樂觀鎖（逾期掃描與付款衝突時，樂觀鎖讓一方失敗，付款方優先，批次任務跳過衝突帳單）

### PropertyAccount Aggregate（Billing BC）

- **Root Entity**：PropertyAccount（一個物業一個）
- **包含**：當月未結算的 AccountingEntry entities
  - 來源：BillPaid、JournalExpenseRecorded、DepositRefunded、DepositDeducted events
  - `category: rent_payment | electricity_payment | deposit_refund | deposit_deduction | journal_expense`（財報分類用）
- **寫入邊界**：只載入當月資料，不載入歷史分錄
- **月結快照**：每月月底排程執行，將當月 AccountingEntry 封存為 `monthly_snapshot_entries`，清空 Aggregate 內當月暫存資料
- **初始化**：物業建立時自動建立，生命週期與物業綁定
- **刪除策略**：物業軟刪除時 PropertyAccount 封存，MonthlySnapshot 永久保留
- **讀取策略**：
  - 當月：讀取 PropertyAccount 當月 AccountingEntry
  - 歷史：讀取 `monthly_snapshot_entries`（直接 SQL，不走 Aggregate）
  - 財報：組合當月 + 歷史，依 category 分組顯示

### JournalLog Aggregate（Journal BC）

- **Root Entity**：JournalLog
- **包含**：紀錄內容、費用條目（optional Value Object）
- **無狀態機**
- **費用流程**：記錄費用時發出 `JournalExpenseRecorded` event → PropertyAccount 訂閱新增 AccountingEntry

### RepairRequest Aggregate（Journal BC）

- **Root Entity**：RepairRequest
- **包含**：
  - 報修內容
  - `assignedTo: UserId`（nullable）
  - `submittedAt`
  - `assignedAt`（nullable）
  - `completedAt`（nullable）
- **狀態機**：
  - `submitted → assigned → in_progress → completed`
  - `submitted → cancelled`
  - `assigned → cancelled`
  - `in_progress → cancelled`
- **cancelled 後置處理**：Property BC 訂閱 `RepairCancelled` event，檢查該 Room 是否所有 RepairRequest 均為 `completed` 或 `cancelled`，若是則 Room 改回 `vacant`
- **派工**：指派系統內員工，員工負責線下聯絡廠商
- **刪除限制**：進行中的 RepairRequest 所屬 Room 不得刪除（BR-08）

### User Aggregate（Identity & Access BC）

- **Root Entity**：User
- **包含**：角色（Value Object）、個別權限覆寫清單、物業指派清單、`firebase_uid`
- **一致性邊界**：角色與權限同 User 一起管理；認證狀態由 Firebase 管理，不在此 Aggregate 範圍內
- **併發策略**：樂觀鎖

---

## Domain Events

| Event | 發出 BC | 說明 | 主要 Payload | 訂閱者 |
|-------|---------|------|-------------|-------|
| LeaseCreated | Leasing | 租約建立 | leaseId, roomId, tenantId, startDate, endDate | Property, Billing |
| LeaseTerminated | Leasing | 租約終止（含強制終止） | leaseId, roomId, forced: bool, isRenewal: bool | Property, Billing, Notification |
| LeaseConditionChanged | Leasing | 租金金額調整 | leaseId, newRentAmount | Billing |
| DepositRefunded | Leasing | 押金退還（含部分退還） | leaseId, refundAmount, propertyId | Billing |
| DepositDeducted | Leasing | 押金扣款（含部分扣款） | leaseId, deductionAmount, reason, propertyId | Billing |
| TenantInfoUpdated | Leasing | 租客資料變更 | tenantId | —（預留，審計用） |
| RentBillsGenerated | Billing | 租約建立時預產帳單 | leaseId, tenantEmail, tenantName, roomName, bills[]{amount, dueDate, type} | Notification |
| BillPaid | Billing | 帳單收款確認 | billId, amount, propertyId | PropertyAccount |
| MeterRecorded | Billing | 電表抄錄完成，電費帳單金額更新 | billId, meterReading | — |
| JournalExpenseRecorded | Journal | 日誌費用記錄 | journalLogId, amount, propertyId | Billing |
| RepairCompleted | Journal | 維修完成 | repairRequestId, roomId, propertyId | Property |
| RepairCancelled | Journal | 維修取消 | repairRequestId, roomId, propertyId | Property |
| PropertyCreated | Property | 物業建立 | propertyId | Billing（建立 PropertyAccount） |
| RoomSetToMaintenance | Property | 房間進入維修狀態 | roomId, propertyId, operatorId | — |
| PropertyUnassigned | Identity & Access | 物業指派移除 | userId, propertyId | —（進行中操作不撤銷，下次操作時權限擋住） |

> **v2.2 修正**：
> - LeaseConditionChanged payload 移除 newPaymentDay（付款日不可修改）
> - RepairCompleted payload 補上 roomId（Property BC 需要判斷該 Room 是否所有 RepairRequest 完成）
> - 新增 PropertyUnassigned event
> - 已移除：LeaseRenewed（v2.1）

---

## Business Rules

| 規則編號 | 規則描述 | 適用條件 | 拒絕行為 | 例外情況 |
|---------|---------|---------|---------|---------|
| BR-01 | 租約起始日不得晚於終止日 | 建立或修改租約時 | 拒絕操作 | 無 |
| BR-02 | 租金不得為零 | 建立或變更租約條件時 | 拒絕操作 | 無 |
| BR-03 | 押金金額不得為負 | 建立租約時 | 拒絕操作 | 無 |
| BR-04 | 正常終止：租約終止前所有帳單必須結清 | 執行正常租約終止時 | 拒絕終止，回傳未付帳單清單 | 強制終止（主辦以上）可跳過，未結清帳單標記 written_off |
| BR-05 | 帳單不得重複收款 | 帳單狀態已為 paid | 拒絕收款操作 | 無 |
| BR-06 | 電表度數不得小於上月度數 | 輸入電表抄錄時 | 拒絕抄錄 | 無 |
| BR-07 | 底下有出租中房間的物業不得刪除 | 執行物業刪除時 | 拒絕刪除 | 無 |
| BR-08 | 出租中或維修中的房間不得刪除 | 執行房間刪除時 | 拒絕刪除 | 無 |
| BR-09 | ~~正常退押金前必須確認所有帳單結清~~ | ~~已移除~~ | BR-04 已完整涵蓋此規則，退租流程統一由 BR-04 把關，不重複檢查 | — |
| BR-10 | 押金扣款須填寫扣款原因 | 執行押金扣款時 | 拒絕操作 | 無 |
| BR-11 | 物業指派對象必須為工作室成員 | 指派對象角色為業主 | 拒絕指派 | 無 |
| BR-12 | 建立租約時目標房間必須為 vacant，且需取得 Room 的悲觀鎖後才執行檢查 | 建立租約時 | 拒絕建立 | 無 |
| BR-13 | 系統管理員不得降低自己的角色 | 修改自身角色時 | 拒絕操作 | 無 |
| BR-14 | 強制終止租約需主辦以上角色執行並填寫原因 | 執行強制終止時 | 員工角色拒絕執行 | 無 |
| BR-15 | 付款日固定為租約起始日，特殊月份（無該日）順延至月底 | 帳單預產時 | 自動計算，無需拒絕 | 無 |
| BR-16 | 電費帳單金額由系統計算：usage = currentReading - previousReading，rawAmount = usage × MeterReading.unitPrice，amount = round(rawAmount)；不由員工輸入金額 | 抄表送出時 | 系統自動計算並四捨五入為整數，拒絕員工直接輸入金額 | 無 |
| BR-17 | 修改物業電價（electricityUnitPrice）需主辦以上角色，且電價僅接受大於 0 的數值，可接受小數 | 修改電價時 | 員工角色拒絕；零與負數拒絕 | 無 |
| BR-18 | 附件允許的 MIME type：`image/jpeg`、`image/png`、`image/heic`、`application/pdf`；單檔上限 20MB | 上傳附件時（Step 3 登記） | 拒絕登記，回傳 422 | 無 |

---

## Roles & Permissions

授權模型：混合型（RBAC 基礎功能控管 + Resource-based 物業層級存取控制）

**Resource-based 實作策略**：系統使用 Firebase Auth 管理認證。`role` 與 `assigned_property_ids` 透過 Firebase Custom Claims 存放於 ID token，後端 middleware 以 Firebase Admin SDK 驗證 token 後直接讀取 claims，每個 request 不需要額外查詢 Identity BC。

物業指派變更（`POST /users/{id}/property-assignments`）後，後端須呼叫 Firebase Admin SDK 更新該使用者的 Custom Claims，前端下次 refresh token 後新指派生效。

> **Custom Claims 容量限制**：Firebase Custom Claims 上限為 1000 bytes。`assigned_property_ids` 存放 UUID 陣列，約可容納 20 筆物業指派。若單一成員指派物業數量接近上限，middleware 應改為從 DB 查詢指派清單（搭配 cache），不依賴 token 內的 claims。

| 操作 | 系統管理員 | 主辦 | 員工 | 業主 |
|------|-----------|------|------|------|
| **Identity & Access** | | | | |
| 建立／編輯工作室成員帳號 | ✅ | ❌ | ❌ | ❌ |
| 建立業主帳號 | ✅ | ✅ | ❌ | ❌ |
| 角色調整 | ✅ | ❌ | ❌ | ❌ |
| 物業指派給成員 | ✅ | ❌ | ❌ | ❌ |
| 重設他人密碼（重寄設定密碼信） | ✅ | ❌ | ❌ | ❌ |
| 重設自己密碼（Firebase forgot password） | ✅ | ✅ | ✅ | ✅ |
| **Property** | | | | |
| 建立／編輯物業和房間 | ✅ | ✅ | ✅ | ❌ |
| 修改電價（electricityUnitPrice） | ✅ | ✅ | ❌ | ❌ |
| 刪除物業和房間 | ✅ | ✅ | ❌ | ❌ |
| 查看物業和房間狀態 | ✅（全部） | ✅（指派） | ✅（指派） | ✅（自己名下） |
| 設定房間進入維修 | ✅ | ✅ | ✅ | ❌ |
| **Leasing** | | | | |
| 建立／編輯 Tenant | ✅ | ✅ | ✅ | ❌ |
| 建立／編輯 Lease | ✅ | ✅ | ✅ | ❌ |
| 刪除 Lease | ✅ | ✅ | ❌ | ❌ |
| 正常終止租約 | ✅ | ✅ | ✅ | ❌ |
| 強制終止租約 | ✅ | ✅ | ❌ | ❌ |
| 租金調整（LeaseConditionChanged） | ✅ | ✅ | ❌ | ❌ |
| **Billing** | | | | |
| 查看帳單列表／明細 | ✅（全部） | ✅（指派） | ✅（指派） | ✅（自己名下，唯讀） |
| 電表抄錄 | ✅ | ✅ | ✅ | ❌ |
| 收款確認 | ✅ | ✅ | ✅ | ❌ |
| 查看財報 | ✅（全部） | ✅（指派） | ✅（指派） | ✅（自己名下，唯讀） |
| 財報審核與寄送 | ✅ | ✅ | ❌ | ❌ |
| **Journal** | | | | |
| 建立／編輯 JournalLog | ✅ | ✅ | ✅ | ❌ |
| 刪除 JournalLog | ✅ | ✅ | ❌ | ❌ |
| 建立／編輯 RepairRequest | ✅ | ✅ | ✅ | ❌ |
| 刪除 RepairRequest | ✅ | ✅ | ❌ | ❌ |
| 派工（assignedTo） | ✅ | ✅ | ✅ | ❌ |
| 查看 JournalLog／RepairRequest | ✅（全部） | ✅（指派） | ✅（指派） | ❌ |
| **Read Models** | | | | |
| PropertyOwnerView | ✅（全部） | ✅（指派） | ✅（指派） | ✅（自己名下） |
| 財報摘要（PropertyAccountSummaryQuery） | ✅（全部） | ✅（指派） | ✅（指派） | ✅（自己名下，唯讀） |

> **業主可見範圍說明**：
> - 帳單列表／明細：可見（唯讀）
> - 財報摘要：可見（唯讀）
> - JournalLog / RepairRequest：不可見
> - 所有寫入操作：不可執行

---

## Read Models

> 所有 Read Model 不走 Aggregate，直接 SQL 查詢組合。

### Identity & Access BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /users` | 成員列表 | role |
| `GET /users/{id}` | 成員詳情 | — |
| `GET /users/me` | 自己的帳號資料 | JWT |

### Property BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /properties` | 物業列表（含房間數、出租率） | 依角色 resource filter |
| `GET /properties/{id}` | 物業詳情 | — |
| `GET /properties/{id}/rooms` | 房間列表（含狀態） | status |
| `GET /rooms/{id}` | 房間詳情 | — |

### Leasing BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /tenants` | 租客列表 | — |
| `GET /tenants/{id}` | 租客詳情 | — |
| `GET /tenants/{id}/leases` | 某租客的租約歷史 | — |
| `GET /leases` | 租約列表 | propertyId, roomId, tenantId, status |
| `GET /leases/{id}` | 租約詳情（含帳單預產狀況、押金狀態） | — |

### Billing BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /bills` | 帳單列表 | propertyId, leaseId, status, month |
| `GET /bills/{id}` | 帳單詳情（電費帳單自動帶入 previousReading） | — |
| `GET /properties/{id}/financial-report` | 財報摘要列表（歷史月份） | year |
| `GET /properties/{id}/financial-report/{year}/{month}` | 特定月份財報 | — |

### Journal BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /journal-logs` | 日誌列表 | propertyId |
| `GET /journal-logs/{id}` | 日誌詳情 | — |
| `GET /repair-requests` | 維修列表 | propertyId, roomId, status |
| `GET /repair-requests/{id}` | 維修詳情 | — |

### 電表相關

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /properties/{id}/pending-meter` | 某物業待抄表清單（`pending_meter` 帳單） | — |
| `GET /properties/{id}/meter-history` | 某物業全房間電表歷史，按月呈現 | year |
| `GET /rooms/{id}/meter-history` | 單房間電表歷史 | year, month |

> **previousReading 填入機制**：`GET /bills/{id}` 回傳時，系統自動查詢同一 `room_id` 最近一張 `meter_reading IS NOT NULL` 的電費帳單的 `currentReading`，作為 `previousReading` 帶入 response。員工送出抄表時只傳 `currentReading`。

### 附件查詢

| 查詢路徑 | 說明 |
|---------|------|
| `GET /properties/{id}/attachments` | 物業附件列表 |
| `GET /rooms/{id}/attachments` | 房間附件列表 |
| `GET /tenants/{id}/attachments` | 租客附件列表 |
| `GET /leases/{id}/attachments` | 租約附件列表 |
| `GET /journal-logs/{id}/attachments` | 日誌附件列表 |
| `GET /repair-requests/{id}/attachments` | 維修單附件列表（依 sort_order 排序） |
| `GET /bills/{id}/attachments` | 帳單附件列表 |
| `DELETE /attachments/{id}` | 軟刪除附件 |

### 跨 BC 組合查詢

| 查詢路徑 | 說明 | 資料來源 |
|---------|------|---------|
| `GET /properties/{id}/dashboard` | PropertyOwnerView：房間狀態 + 本月收支 + 逾期帳單數 + 最近 5 筆 Journal | Property + Billing + Journal |

### PropertyOwnerView 內容

```
PropertyOwnerView
  ├── 各房間狀態（vacant / occupied / maintenance）
  ├── 本月應收租金總額
  ├── 本月已收租金總額
  ├── 逾期未付帳單數量
  └── 最近 5 筆 Journal 紀錄（JournalLog + RepairRequest）
```

### PropertyAccountSummaryQuery（財報讀取）

直接 SQL 彙總 MonthlySnapshot + 當月 AccountingEntry，按月分組產出收支報表。

---

## 資料庫設計原則

| 項目 | 決策 |
|------|------|
| 刪除策略 | 全部軟刪除，加 `deleted_at` 欄位 |
| 軟刪除 filter | 所有查詢（含排程任務）加 `AND deleted_at IS NULL`，確保軟刪除物業的帳單不被掃到 |
| 幣別 | 台幣，金額用整數儲存（無小數） |
| 帳單主要 Index | `(property_id, status, due_date)` |
| 帳單次要 Index | `(tenant_id, status)`、`(status, due_date)` |
| 帳單電表 Index | `(property_id, type, due_date)`、`(room_id, type, due_date)` |
| 帳單催收 Index | `(status, overdue_notice_count)` on bills table |
| 租約 Index | `(property_id, status)`、`(tenant_id)`、`(end_date, status)` on leases table |
| Tenant Index | `(status)` on tenants table |
| 維修 Index | `(property_id, status)` on repair_requests table |
| 日誌 Index | `(property_id, created_at)` on journal_logs table |
| ForceTermination table | 欄位：`id, lease_id, initiated_by, reason, status: in_progress \| completed, created_at`（bill_ids[] 移除，改用 force_termination_bills table） |
| PropertyAccount 架構 | 月結快照拆兩層：summary + entries |
| monthly_snapshots table（summary） | 欄位：`id, property_id, year, month, total_income, total_expense, net`；Index：`(property_id, year, month)` |
| monthly_snapshot_entries table（明細） | 欄位：`id, snapshot_id, category, description, amount, source_ref`；Index：`(snapshot_id, category)` |
| MonthlySnapshot 排程 | 排除軟刪除物業：加 `AND deleted_at IS NULL` filter |
| force_termination_bills table | 欄位：`id, force_termination_id, bill_id, status: pending \| done`；Index：`(force_termination_id, status)`；補償排程查 `WHERE status = pending` |
| voided / written_off 帳單 | 狀態轉換（非軟刪除），查詢加 `AND status NOT IN ('voided', 'written_off')` |
| 資料量預估 | 50 房間 × 24 個月 = 1200 筆預產帳單，現有 Index 足夠，不需分表 |
| users table 認證欄位 | 存 `firebase_uid VARCHAR(128) UNIQUE NOT NULL`，不存 `password_hash`；密碼由 Firebase 管理 |

---

## 退租流程

### 正常退租

```
發起退租
  → 檢查所有帳單是否結清
  → 若有未付帳單 → 拒絕，回傳未付帳單清單
  → 全部結清 → 確認押金處理（退還或扣款）
  → DepositRefunded / DepositDeducted event → PropertyAccount
  → LeaseTerminated event（forced: false）
  → Property BC 訂閱 → Room 狀態改為 vacant
  → Notification BC 訂閱 → 寄送退租確認 Email
```

### 強制終止（呆帳）

```
主辦以上角色發起強制終止，填寫原因
  → 記錄 ForceTerminationStarted（leaseId, billIds[], reason）
  → 逐一將未結清帳單標記為 written_off，每筆成功記錄進度
  → 若中途失敗 → 排程任務掃描未完成的 ForceTermination，繼續補償
  → 全部 written_off 完成 → 押金標記 settled 或 written_off（人工決定）
  → LeaseTerminated event（forced: true）
  → Property BC 訂閱 → Room 狀態改為 vacant
  → Notification BC 訂閱 → 寄送強制終止通知
```

> 最後一個月帳單按整月計算，不按天拆分。
> 空窗期（vacant 期間）不產生任何帳單，超出系統範圍。

---

## 附件設計

### 設計原則

附件為純 CRUD 輔助資料，不參與任何 BC 的業務規則或 Aggregate 狀態機。生命週期跟隨宿主資源（宿主軟刪除時，應用層於同一 DB transaction 內同步軟刪除其附件）。

### 支援附件的資源

| 資源 | 附件表 | 備註 |
|------|--------|------|
| Property | `property_attachments` | 物業照片 |
| Room | `room_attachments` | 房間照片 |
| Tenant | `tenant_attachments` | 身份文件等 |
| Lease | `lease_attachments` | 合約掃描等 |
| JournalLog | `journal_log_attachments` | 日誌附件、費用憑證等 |
| RepairRequest | `repair_request_attachments` | 維修照片 |
| Bill | `bill_attachments` | 電表照片等 |

### 附件表 Schema

**共用欄位**（所有附件表均包含）：

```
id UUID PRIMARY KEY
object_path TEXT NOT NULL          -- GCS object path（非完整 URL），如 attachments/properties/{id}/{uuid}.jpg
file_name TEXT NOT NULL            -- 原始檔名（顯示用）
uploaded_by UUID REFERENCES users(id)
created_at TIMESTAMPTZ NOT NULL
deleted_at TIMESTAMPTZ
```

**`repair_request_attachments` 額外欄位**：

```
sort_order INT NOT NULL DEFAULT 0
photo_stage VARCHAR(20) CHECK (photo_stage IN ('before', 'after', 'other'))
```

**Index**：各附件表的 FK 欄位均加 Index（`property_id`、`room_id`、`tenant_id`、`lease_id`、`journal_log_id`、`repair_request_id`、`bill_id`）。`repair_request_attachments` 加 `(repair_request_id, sort_order)`。

### 上傳流程（Signed URL + nonce 綁定）

```
Step 1：POST /attachments/upload-url
  Body: { resource_type, resource_id, file_name, content_type }
  → 後端驗證呼叫者對 resource 有寫入權限
  → 後端建立暫存 token（nonce, object_path, issued_to, resource_type, resource_id, expires_at）
  → 回傳 { upload_url, nonce, expires_at }（Signed URL 含鎖定 content_type）

Step 2：Client 直接 PUT 到 GCS（不經後端）

Step 3：POST /{resource}/{id}/attachments
  Body: { nonce, file_name }
  → 後端以 nonce 驗證合法性（issued_to、resource_id 需一致）
  → 後端呼叫 GCS HEAD 確認物件存在（BR-18 MIME type 驗證）
  → 寫入對應 attachment table，刪除已用 nonce
```

**upload_tokens 暫存表**：

```
attachment_upload_tokens(
  id UUID PRIMARY KEY,
  nonce VARCHAR UNIQUE NOT NULL,
  object_path TEXT NOT NULL,
  issued_to UUID NOT NULL,
  resource_type VARCHAR NOT NULL,
  resource_id UUID NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
)
```

**部署前置條件**：GCS bucket 需設定 CORS policy（允許前端 origin，methods: PUT，headers: Content-Type）；後端 Service Account 需具備 `storage.objects.create`、`storage.objects.get` 權限。

---

## 排程任務

| 任務 | 頻率 | 說明 |
|------|------|------|
| 逾期帳單掃描 | 每日凌晨 | 掃描 `due_date < today AND status = pending_payment AND deleted_at IS NULL`，批次更新為 `overdue`。與付款衝突時樂觀鎖失敗，跳過該筆。 |
| 逾期帳單催收通知 | 每週一次 | 掃描 `status = overdue AND deleted_at IS NULL`，對租客寄催收 Email，每張帳單最多寄 3 次（記錄 `overdue_notice_count`）。 |
| 租約到期掃描 | 每日凌晨 | 掃描 `end_date < today AND status = active AND deleted_at IS NULL`，批次更新 Lease status 為 `expired`，Room 維持 `occupied`，不自動終止。 |
| 租約到期提醒 | 每日凌晨 | 掃描 `end_date = today + 30 days AND status = active`，寄送提醒 Email 給主辦和員工。 |
| 月結快照產出 | 每月最後一天 23:59 | 將 PropertyAccount 當月 AccountingEntry 封存為 `monthly_snapshot_entries`，排除軟刪除物業（`deleted_at IS NULL`），清空當月暫存資料。 |
| 強制終止補償 | 每日凌晨 | 掃描 `force_terminations WHERE status = in_progress`，續行未完成的 written_off 操作。 |
| Upload token 清理 | 每日凌晨 | 清除 `attachment_upload_tokens WHERE expires_at < now`。 |
| GCS 附件清理 | 每日凌晨 | 掃描 `*_attachments WHERE deleted_at < now - INTERVAL '90 days'`，刪除對應 GCS 物件後硬刪除 DB 記錄。 |

---

## 設計決策紀錄（ADR）

| 決策 | 選擇 | 理由 |
|------|------|------|
| 押金建模 | Lease 內的 Value Object | 押金生命週期完全跟著租約，不需獨立 Aggregate |
| 續約處理 | LeaseTerminated + LeaseCreated | 避免 LeaseRenewed 語意模糊，帳單與押金歷史清晰 |
| 電費帳單建立 | 租約建立時一併預產，初始 pending_meter | 與租金帳單同批預產，MeterRecorded 時找對應帳單更新金額 |
| 租金調整後帳單 | void `due_date >= nextPaymentDate` 的帳單，從 nextPaymentDate 重產 | 當月帳單不動，業務語意清楚（這個月金額已說好）；nextPaymentDate 計算複用 BR-15 邏輯 |
| 付款日 | 固定為租約起始日，不可單獨修改，特殊月份順延月底 | 消除帳單產生邏輯的邊界案例，paymentDay 欄位移除 |
| JournalEntry 拆分 | JournalLog + RepairRequest | 兩者行為差異過大，合併導致 schema nullable 欄位過多 |
| PropertyAccount 架構 | 月結快照：Aggregate 只持有當月，歷史走 MonthlySnapshot | 解決 Aggregate 無限增長問題，寫入效能穩定 |
| 租客角色 | 無系統帳號，Email 通知 | 降低系統複雜度，租客透過 Email 收帳單 |
| 代管費 | 超出範圍 | 工作室財務另外處理，不在 STDS 範圍內 |
| 跨 BC 傳遞 | In-process event bus | 單一服務架構，無需 message queue |
| 軟刪除 | 全部軟刪除，所有查詢加 deleted_at IS NULL filter | 歷史帳單和日誌需保留關聯，不可真刪除 |
| 強制終止租約 | 支援補償機制（saga 雛形），ForceTerminationStarted 記錄進度 | 跨多個 Bill Aggregate，中途失敗可由排程續行 |
| Room 競態防護 | 建立租約時對 Room 取悲觀鎖（select for update）再檢查 BR-12 | 防止兩個請求同時對同一 Room 建立租約 |
| maintenance → vacant 條件 | 該 Room 所有 RepairRequest 均 completed 才轉回 vacant | 多個 RepairRequest 場景下避免過早開放房間 |
| BR-09 移除 | 移除，BR-04 已完整涵蓋 | 退租流程統一由 BR-04 把關，不重複檢查 |
| 物業指派移除 event | 新增 PropertyUnassigned event，進行中操作不撤銷 | 下次操作時權限自然擋住，不需要複雜的操作撤銷邏輯 |
| BR-08 維修中刪除 | 移除例外，維修中房間不得刪除 | 刪除維修中 Room 導致 RepairRequest 狀態機孤立 |
| 逾期競態 | 樂觀鎖，付款優先，批次跳過衝突 | 帳單已付款則批次自然不再掃到，無需額外處理 |
| overdue → paid | 允許 | 逾期帳單仍應可收款，不因逾期狀態阻斷收款流程 |
| 押金部分扣款 | Deposit VO 拆分 deductionAmount + refundAmount，status 改為 settled 取代 refunded/deducted | 台灣退租最常見情境是「扣一部分、退餘額」，原 refunded/deducted 二選一無法表達；DepositDeducted + DepositRefunded 兩事件可依序發出，PropertyAccount 分別記入 deposit_deduction 與 deposit_refund 分錄 |
| 續約誤發退租通知 | LeaseTerminated 加 isRenewal: bool，Notification BC 僅在 isRenewal: false 時寄退租確認 | 續約操作（舊租約終止 + 新租約建立）會觸發 LeaseTerminated，若不加識別欄位，租客會收到錯誤的退租確認 Email |
| 認證機制 | Firebase Auth + Custom Claims | 降低自建 JWT 與密碼管理複雜度；Custom Claims 支援 server-side 更新，可在物業指派變更後立即同步 role 與 assigned_property_ids 至 token；users table 改存 firebase_uid 取代 password_hash |
| 附件業務定位 | 純 CRUD，不參與業務規則 | 附件為輔助資訊，不影響任何 Aggregate 狀態機或業務決策；保持模型簡潔，避免過度設計 |
| 附件表架構 | 方案 B：各資源獨立附件表 | 維持真實 FK 約束，符合現有 BC 邊界設計風格；軟刪除 cascade 由應用層 transaction 保證 |
| 附件上傳機制 | Signed URL + nonce 綁定 | Client 直傳 GCS 減少後端 I/O；nonce 機制防止任意 object_path 注入；Step 3 GCS HEAD 確認物件存在 |
| GCS 部署依賴 | CORS 與 IAM 為部署前置條件（accepted risk） | Signed URL 直傳需要瀏覽器 CORS 支援；Service Account 權限不足會導致 URL 產生失敗；兩者屬基礎設施設定，在首次部署前完成 |
