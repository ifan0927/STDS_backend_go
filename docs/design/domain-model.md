Domain Model

> 版本：v3.4
> 更新說明：
> - v2.0：經四輪多角色設計評審產出
> - v2.1-v2.8：歷次 Validation 修正
> - v2.9：補 paymentMethod 欄位、expired → terminated 狀態轉換、Tenant status 轉換邏輯、monthly_snapshots 拆兩層 table、force_termination_bills 拆表移除 bill_ids[]、補 overdue_notice_count Index、Notification event payload 要求
> - v3.0：認證機制改為 Firebase Auth + Custom Claims，移除自建 JWT 與 password_hash，users table 改存 firebase_uid
> - v3.2：新增共用附件機制（GCS Signed URL + nonce 綁定 + 各資源獨立附件表），補 BR-18、附件相關 Read Models、排程任務、ADR
> - v3.3：電費單價改為可接受浮點數；電費帳單 amount 維持台幣整數，計算後採四捨五入
> - v3.4：補事件邊界分類矩陣，對齊目前 in-process post-commit event bus 與 direct orchestration 邊界

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

租約建立時，系統於整個租約期間預產所有帳單（租金帳單 + 電費帳單）。租金帳單初始狀態為 `pending_payment`，電費帳單初始狀態為 `pending_meter`，等待該帳單所屬 billing period 抄表後更新金額。

**付款日語意**：付款日固定為租約起始日當天，每月同一天。該月無此日期（如 1/31 → 2 月）則順延至該月最後一天。付款日不可單獨修改，屬於租約起始條件的一部分。

Lease 於建立時決定 `electricityBillingCadence`（`monthly | bimonthly`）。若 request 未帶值，套用 Property 的 `defaultElectricityBillingCadence`。Lease 建立後 cadence 不可修改；若需更動 cadence、續約重簽、或其他少數條件重建，走 `LeaseReplaced` 流程，以「終止舊 Lease + 建立新 Lease」處理。

租金調整（`LeaseConditionChanged`）時，void 所有 `due_date >= nextPaymentDate` 且狀態為 `pending_payment` 或 `pending_meter` 的帳單（狀態改為 `voided`），從 `nextPaymentDate` 起重產新金額帳單。`nextPaymentDate` 為 operationDate 之後的第一個付款日，計算規則同 BR-15。當月帳單不受影響，即使尚未到期。`LeaseConditionChanged` 僅涵蓋租金金額調整，不含付款日修改與 cadence 修改。

### Billing

電表抄錄、帳單管理與收款確認。帳單分為租金帳單與電費帳單，電費帳單在租約建立時即依 `electricityBillingCadence` 預產（`pending_meter`）。抄表 command 在同一 transaction 內計算用電金額並將帳單更新為 `pending_payment`，`MeterRecorded` event 僅保留為 domain trace / future extension。

每日排程任務掃描逾期帳單（`due_date < today AND status = pending_payment`），批次更新為 `overdue`。允許 `overdue → paid` 轉換（逾期帳單仍可收款）。

收款確認後在同一 transaction 內產生 `AccountingEntry`；`BillPaid` event 不負責 PropertyAccount 寫入，僅保留為 domain trace / future notification candidate。
收款金額必須等於帳單金額；系統不支援部分收款或溢收。可收款狀態限 `pending_payment` 與 `overdue`，其中 `overdue → paid` 明確允許。

**LeaseTerminated 後的處理**：目前沒有 Billing BC runtime subscriber。正常終止時帳單應已全清；強制終止時未結清帳單在 command 內同步標記 `written_off`、記錄 force termination progress，並完成 force termination。若未來需要處理剩餘預產帳單 void，應先確認是否需與 termination command 強一致。

**強制終止租約**（呆帳情境）：由主辦以上角色執行，流程記錄於 `force_terminations` table（Billing BC），在同一 command 中同步將未結清帳單標記為 `written_off`、記錄 `deposit_handling` 決策、完成 ForceTermination，並發出 `LeaseTerminated`。

### Journal

分為兩個子模組：

- **JournalLog**：純文字紀錄與費用記錄（如日常維護費用），無狀態機
- **RepairRequest**：報修派工，有完整狀態機，派工對象為系統內員工（員工負責線下聯絡廠商）

### Notification

租客無系統帳號，所有通知以 Email 發送。通知發送紀錄不存儲，發出即視為完成。

**通知觸發時機：**

| 通知類型 | 觸發時機 | 收件人 | 觸發來源 |
|---------|---------|-------|---------|
| 帳單通知 | 帳單預產完成 | 租客 | 尚未承諾；`RentBillsGenerated` 不存在於目前 runtime event structs |
| 收款收據 | 帳單收款確認（`BillPaid`） | 租客 | 尚未承諾；`BillPaid` 目前只保留為 trace / future notification candidate |
| 逾期催收通知 | 每週排程，最多 3 次 | 租客 | 排程任務 |
| 退租確認 | `LeaseTerminated`（forced: false **且** isReplacement: false） | 租客 | 尚未承諾 |
| 強制終止通知 | `LeaseTerminated`（forced: true） | 租客 | 尚未承諾 |
| 租約到期提醒 | 到期前 30 天排程 | 主辦、員工 | 排程任務 |
| 財報寄送 | 主辦手動審核後觸發 | 業主 | 應由 send flow direct orchestration 處理 |

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
  - `electricityBillingCadence: monthly | bimonthly`
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
  - Lease replacement：作為特殊條件重建例外，允許 `depositHandling = carry_over`，不在終止舊 Lease 時結清押金
  - 強制終止：主辦以上角色執行，未結清帳單標記 `written_off`，記錄呆帳原因
  - 押金退還／扣款透過 Lease Aggregate 操作
- **併發策略**：樂觀鎖

### Property Aggregate（Property BC）

- **Root Entity**：Property
- **包含**：
  - Room entities（含狀態）
  - `electricityUnitPrice`（台幣正數/度，物業層級電價，可接受小數，例：4.5）
  - `defaultElectricityBillingCadence: monthly | bimonthly`
- **Room 狀態**：`vacant | occupied | maintenance`
- **狀態轉換**：
  - `vacant → occupied`：訂閱 `LeaseCreated`
  - `occupied → vacant`：訂閱 `LeaseTerminated`
  - `vacant → maintenance`：主辦或員工透過 `POST /rooms/{id}/maintenance` 手動操作；同一 transaction 內建立 room-scoped `RepairRequest`，並發出 `RoomSetToMaintenance`
  - `maintenance → vacant`：應由 repair workflow direct orchestration 檢查該 Room 所有 RepairRequest 均為 `completed` 或 `cancelled` 後改回 `vacant`；目前 runtime 尚未實作此後置處理，不應依賴沒有 durable guarantee 的 post-commit subscriber 承擔此強一致性狀態更新
- **一致性邊界**：房間隨物業刪除而刪除；房間狀態由 Property Aggregate 統一管理
- **併發策略**：樂觀鎖

### Bill Aggregate（Billing BC）

- **Root Entity**：Bill
- **包含**：
  - `type: rent | electricity`（預產時帶入）
  - `room_id`（從 Lease 取得，直接存入，電表查詢不需 JOIN）
  - `lease_id`
  - `periodStart`
  - `periodEnd`
  - 付款紀錄（Value Object）
    - `paymentMethod: cash | transfer | other`（收款確認時填入）
    - `paidAt`
    - `paidAmount`
  - 帳單狀態
  - `meterReading: MeterReading`（optional Value Object，電費帳單專用）
    - `previousReading`（系統查詢填入，不由員工輸入；取同 room 最近一張已完成上一個 electricity billing period 的帳單）
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
- **收款規則**：`paidAmount` 必須等於帳單 `amount`；不支援部分收款或溢收
- **金額儲存**：台幣整數（無小數）
- **電費換算規則**：`rawAmount = usage × MeterReading.unitPrice`，`amount = round(rawAmount)`（四捨五入為最終帳單金額）
- **併發策略**：樂觀鎖（逾期掃描與付款衝突時，樂觀鎖讓一方失敗，付款方優先，批次任務跳過衝突帳單）

### PropertyAccount Aggregate（Billing BC）

- **Root Entity**：PropertyAccount（一個物業一個）
- **包含**：當月未結算的 AccountingEntry entities
  - 來源：Bill payment command 直接寫入；Journal expense command 直接寫入並保留 `JournalExpenseRecorded` 作 trace/reserved event；DepositRefunded / DepositDeducted 由押金處理 command 直接寫入
  - `category: rent_payment | electricity_payment | deposit_refund | deposit_deduction | journal_expense`（財報分類用）
- **寫入邊界**：只載入當月資料，不載入歷史分錄
- **月結快照**：每月月底排程執行，將當月 AccountingEntry 封存為 `monthly_snapshot_entries`，清空 Aggregate 內當月暫存資料
- **初始化**：物業建立時由 property creation command direct orchestration 自動建立，生命週期與物業綁定
- **刪除策略**：物業軟刪除時 PropertyAccount 封存，MonthlySnapshot 永久保留
- **讀取策略**：
  - 當月：讀取 PropertyAccount 當月 AccountingEntry
  - 歷史：讀取 `monthly_snapshot_entries`（直接 SQL，不走 Aggregate）
  - 財報：組合當月 + 歷史，依 category 分組顯示

### JournalLog Aggregate（Journal BC）

- **Root Entity**：JournalLog
- **包含**：紀錄內容、費用條目（optional Value Object）
- **無狀態機**
- **費用流程**：記錄費用時，Journal command 在同一個 transaction 直接新增 AccountingEntry，並保留 `JournalExpenseRecorded` 作 trace/reserved event；accounting state 不依賴 post-commit subscriber。

### RepairRequest Aggregate（Journal BC）

- **Root Entity**：RepairRequest
- **包含**：
  - `title`
  - `description`
  - `assignedTo: UserId`（nullable）
  - `submittedAt`
  - `assignedAt`（nullable）
  - `completedAt`（nullable）
- **建立入口**：可由 Journal BC 的 `POST /repair-requests` 建立，也可由 Property BC 的 `POST /rooms/{id}/maintenance` 在同一 transaction 內建立 room-scoped RepairRequest 並同步將 Room 設為 `maintenance`
- **狀態機**：
  - `submitted → assigned → in_progress → completed`
  - `submitted → cancelled`
  - `assigned → cancelled`
  - `in_progress → cancelled`
- **cancelled / completed 後置處理**：Room 狀態恢復應由 repair workflow direct orchestration 處理；目前 `RepairCompleted` / `RepairCancelled` 已發出 event，但沒有 runtime subscriber。
- **派工**：指派系統內員工，員工負責線下聯絡廠商
- **與 Room 狀態關聯**：只要該 Room 尚有未完成且未取消的 RepairRequest，Room 應維持 `maintenance`；因此 BR-08 可由 Room 狀態統一表達，不另定義獨立刪除規則

### User Aggregate（Identity & Access BC）

- **Root Entity**：User
- **包含**：角色（Value Object）、個別權限覆寫清單、物業指派清單、`firebase_uid`
- **一致性邊界**：角色與權限同 User 一起管理；認證狀態由 Firebase 管理，不在此 Aggregate 範圍內
- **併發策略**：樂觀鎖

---

## Domain Events

目前 runtime 使用 synchronous in-process post-commit event bus。`txRunner` 在 command transaction commit 成功後才 publish recorded events；subscriber 失敗不能 rollback 原 command，且沒有 outbox、retry queue、dead-letter queue、durable delivery guarantee 或外部 broker。

分類定義：

- `implemented pub/sub and intentionally kept`：目前有 runtime subscriber，且此專案接受 post-commit follow-up 風險。
- `implemented pub/sub but future consistency concern`：目前有 runtime subscriber，但 side effect 屬於較強一致性資料，未來宜改 direct orchestration。
- `command-owned direct orchestration and intentionally kept`：目前已由 command transaction 直接完成，或決策上確認應由 command transaction 直接完成，不應改成目前這種 post-commit pub/sub。
- `published-only/reserved`：event 可作 domain trace、audit 或 future extension；目前沒有 required runtime subscriber。
- `missing required subscriber candidate`：文件或產品語意看似要求 downstream behavior，但目前 runtime 未實作；後續仍需確認 boundary。
- `obsolete event expectation`：現有實作已採其他路徑，event/subscriber 不應再被視為主流程承諾。
- `decision needed`：目前不承諾 runtime behavior，需另行確認。

| Event / side effect | Current runtime implementation | Classification | Transaction consistency reason | Follow-up status |
|---------------------|--------------------------------|----------------|-------------------------------|------------------|
| `LeaseCreated -> room occupied` | `LeaseCreated` is recorded by create / replacement lease commands; `OccupyRoomOnLeaseCreatedHandler` is wired in `server.New()` | implemented pub/sub and intentionally kept | Room state is updated after lease commit; this project accepts the post-commit inconsistency risk for now | Verify in #49, including replacement sequence |
| `LeaseCreated -> tenant active` | `ActivateTenantOnLeaseCreatedHandler` is wired in `server.New()` | implemented pub/sub and intentionally kept | Tenant reactivation is post-commit follow-up; current project risk is acceptable | Verify in #49 |
| `LeaseTerminated -> room vacant` | Normal termination, force termination, and replacement old-lease termination record `LeaseTerminated`; `ReleaseRoomOnLeaseTerminatedHandler` is wired | implemented pub/sub and intentionally kept | Room release happens after lease commit; replacement emits `LeaseTerminated` before `LeaseCreated`, so tests must confirm final state | Verify in #49, especially replacement |
| `LeaseTerminated -> tenant inactive` | `DeactivateTenantOnLeaseTerminatedHandler` is wired and checks remaining active / expired leases | implemented pub/sub and intentionally kept | Post-commit tenant deactivation is accepted; replacement should not deactivate when successor lease exists | Verify in #49 |
| Lease creation / replacement bill pre-generation | Lease create and replacement commands directly create rent and electricity bills in the lease transaction | command-owned direct orchestration and intentionally kept | Lease and initial bills should succeed or fail together | No subscriber needed |
| Lease rent adjustment bill cleanup / regeneration | `UpdateLeaseService` directly voids future rent bills and creates replacement rent bills | command-owned direct orchestration and intentionally kept | Rent change and generated bill state are one business transaction | No subscriber needed |
| `LeaseConditionChanged` | Event is recorded after rent adjustment; no runtime subscriber | published-only/reserved | Bill regeneration is already direct; event is trace/future extension only | No implementation commitment |
| `LeaseReplaced` | Event is recorded after replacement; no runtime subscriber | published-only/reserved | Replacement state changes and bill generation are already direct; notification/audit behavior is not required now | No implementation commitment |
| Normal termination deposit settlement | Termination command directly settles deposit state, creates deposit accounting entries, then records `DepositRefunded` / `DepositDeducted` when applicable | command-owned direct orchestration and intentionally kept | Lease termination, deposit settlement, and deposit accounting must stay atomic | No subscriber needed |
| `DepositRefunded` / `DepositDeducted -> accounting entry` | Deposit settlement commands directly create AccountingEntry rows; events are recorded only as trace / future extension signals | command-owned direct orchestration and intentionally kept | Deposit accounting is financial state and must stay atomic with lease deposit settlement | Implemented by #51; no subscriber needed |
| Force termination write-off and completion | `ForceTerminateLeaseService` directly writes off bills, marks force termination bills done, completes force termination, then records `LeaseTerminated` | command-owned direct orchestration and intentionally kept | Write-off progress and force termination completion must be atomic with the command | No subscriber needed |
| `BillPaid -> accounting entry` | `RecordPaymentService` directly creates AccountingEntry in the payment transaction, then records `BillPaid` | command-owned direct orchestration and intentionally kept | Payment and accounting entry must succeed or fail together | No subscriber needed |
| `BillPaid` receipt notification | `BillPaid` is recorded; no receipt notification subscriber exists | published-only/reserved | Receipt notification is not committed as required behavior now; do not overload accounting event semantics | No implementation commitment |
| `MeterRecorded` bill amount / status update | `RecordMeterService` directly calculates amount and updates bill state, then records `MeterRecorded` | command-owned direct orchestration and intentionally kept | Meter command's core output is the updated bill; it must be atomic | Event remains trace/reserved |
| `JournalExpenseRecorded -> accounting entry` | Journal expense creation command directly creates AccountingEntry in the same transaction; `JournalExpenseRecorded` remains published-only/reserved with no accounting subscriber | command-owned direct orchestration and intentionally kept | Journal expense and accounting entry must succeed or fail together | Implemented by #53; no subscriber needed |
| `PropertyCreated -> PropertyAccount` | Property creation directly creates PropertyAccount in the same command transaction, then records `PropertyCreated` as trace / future extension | command-owned direct orchestration and intentionally kept | PropertyAccount lifecycle is strongly tied to property creation | No subscriber needed |
| `RoomSetToMaintenance` | `SetRoomMaintenanceService` directly creates room-scoped RepairRequest and sets room to maintenance, then records event | command-owned direct orchestration and intentionally kept | Room state and repair request creation must be atomic | Event remains trace/reserved; no subscriber needed |
| `RepairCompleted` / `RepairCancelled -> room vacant` | Repair workflow records events, but room status recovery is not implemented yet | command-owned direct orchestration and intentionally kept | Room recovery affects Property aggregate state and should be strongly consistent with repair workflow | Future implementation should add direct orchestration |
| `TenantInfoUpdated` | Tenant update records event; no runtime subscriber | published-only/reserved | No required downstream behavior currently exists | No implementation commitment |
| `FinancialReportSendRequested` | Report send service publishes event directly after validation; no runtime subscriber sends report | obsolete event expectation | User-facing send behavior should be explicit; current event path has no delivery behavior | Future implementation should use direct send/orchestration |
| `UserPasswordResetRequested` | Subscriber is wired, but current IAM create-user and password-reset flows call notification directly and do not publish this event | obsolete event expectation | Existing direct notification flow controls request failure semantics and is acceptable | Do not treat event path as required |
| Scheduler overdue reminder notification | Job runner directly increments overdue notice count and sends notification in the job flow | command-owned direct orchestration and intentionally kept | Job summary depends on notification success/failure accounting | No event subscriber needed |
| Scheduler lease expiring-soon notification | Job runner directly calls notification service per recipient | command-owned direct orchestration and intentionally kept | Job-level success/failure is owned by the scheduler use case | No event subscriber needed |

> `PropertyUnassigned` and `RentBillsGenerated` are older design expectations but do not currently exist as event structs in `internal/domain/events`; they should not be treated as runtime commitments unless reintroduced by a focused issue.

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
| BR-18 | 附件允許的 MIME type：`image/jpeg`、`image/png`、`image/heic`、`application/pdf`；單檔上限 20MB | 產生 upload URL 時先驗證；Step 3 登記時再以 GCS metadata 防呆確認 | 拒絕產生 upload URL 或拒絕登記，回傳 422 | 無 |
| BR-19 | Property 預設電費 cadence 僅影響新建 Lease；既有 Lease 不可直接修改 cadence | 修改物業預設 cadence 或編輯 Lease 時 | 拒絕以編輯 Lease 方式修改 cadence | 若需更動 cadence，走 LeaseReplaced |
| BR-20 | Lease replacement 僅允許在完整 electricity billing period boundary 執行 | 執行 LeaseReplaced 時 | 拒絕 replacement | 無 |
| BR-21 | Lease replacement 前，舊 Lease 在 boundary 前的帳單必須全部結清，且不得存在 `pending_meter`、`pending_payment`、`overdue` | 執行 LeaseReplaced 時 | 拒絕 replacement，回傳未結清帳單清單 | 無 |
| BR-22 | Lease replacement 僅支援同 tenant、同 room、同 property 的條件重建，不得用於搬房或換租客 | 執行 LeaseReplaced 時 | 拒絕 replacement | 無 |
| BR-23 | Lease replacement 第一版僅支援 `depositHandling = carry_over`，不得在 replacement 當下結清或改寫押金狀態 | 執行 LeaseReplaced 時 | 拒絕 replacement | 需要押金狀態改變時，走其他流程 |

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
| 建立 Lease | ✅ | ✅ | ✅ | ❌ |
| Lease replacement | ✅ | ✅ | ✅ | ❌ |
| 編輯 Lease（僅租金調整） | ✅ | ✅ | ❌ | ❌ |
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

Operational lease lifecycle changes use terminate, force-terminate, or replacement flows; generic lease deletion is not part of the public API.

### Billing BC

| 查詢路徑 | 說明 | 主要 Filter |
|---------|------|------------|
| `GET /bills` | 帳單列表 | propertyId, leaseId, status, month |
| `GET /bills/{id}` | 帳單詳情（電費帳單自動帶入 previousReading 與 period） | — |
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
| `GET /properties/{id}/pending-meter` | 某物業待抄表清單（`pending_meter` 帳單，按 bill period 顯示） | — |
| `GET /properties/{id}/meter-history` | 某物業全房間電表歷史，按月呈現 | year |
| `GET /rooms/{id}/meter-history` | 單房間電表歷史 | year, month |

> **previousReading 填入機制**：`GET /bills/{id}` 回傳時，系統自動查詢同一 `room_id` 最近一張屬於上一個已完成 electricity billing period 且 `meter_reading IS NOT NULL` 的電費帳單的 `currentReading`，作為 `previousReading` 帶入 response。員工送出抄表時只傳 `currentReading`。

### 附件查詢

| 查詢路徑 | 說明 |
|---------|------|
| `GET /properties/{id}/attachments` | 物業附件列表 |
| `GET /rooms/{id}/attachments` | 房間附件列表 |
| `GET /tenants/{id}/attachments` | 租客附件列表（沿用既有 tenant flow 的 property access 規則） |
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
| ForceTermination table | 欄位：`id, lease_id, initiated_by, reason, deposit_handling: write_off \| keep_held, status: completed, created_at`（bill_ids[] 移除，改用 force_termination_bills table；`in_progress` 為已棄用的補償流程歷史狀態） |
| PropertyAccount 架構 | 月結快照拆兩層：summary + entries |
| monthly_snapshots table（summary） | 欄位：`id, property_id, year, month, total_income, total_expense, net`；Index：`(property_id, year, month)` |
| monthly_snapshot_entries table（明細） | 欄位：`id, snapshot_id, category, description, amount, source_ref`；Index：`(snapshot_id, category)` |
| MonthlySnapshot 排程 | 排除軟刪除物業：加 `AND deleted_at IS NULL` filter |
| force_termination_bills table | 欄位：`id, force_termination_id, bill_id, status: pending \| done`；Index：`(force_termination_id, status)`；同步完成後所有追蹤帳單應為 `done` |
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
  → 更新押金狀態；DepositRefunded / DepositDeducted event 僅保留為 trace
  → LeaseTerminated event（forced: false）
  → Property BC 訂閱 → Room 狀態改為 vacant
```

### 強制終止（呆帳）

```
主辦以上角色發起強制終止，填寫原因
  → 建立 ForceTermination 記錄（leaseId, billIds[], reason, deposit_handling）
  → 同步將未結清帳單標記為 written_off，每筆成功記錄進度
  → 全部成功後將 ForceTermination 標記 completed
  → 全部 written_off 完成 → 依 deposit_handling 人工決定將押金標記 written_off，或維持 held 等待後續押金處理
  → LeaseTerminated event（forced: true）
  → Property BC 訂閱 → Room 狀態改為 vacant
```

> 最後一個月帳單按整月計算，不按天拆分。
> 空窗期（vacant 期間）不產生任何帳單，超出系統範圍。

### Lease Replacement

```
發起 Lease replacement
  → 檢查 replacement reason 與 effective date
  → 檢查新 Lease 與舊 Lease 是否為同 tenant、同 room、同 property
  → 檢查 effective date 是否為完整 electricity billing period boundary
  → 檢查 boundary 前帳單是否已全部結清
  → 檢查 depositHandling = carry_over
  → 正常終止舊 Lease（replacement 專用語意，不在此步結清押金）
  → 建立新 Lease（帶入新條件與 electricityBillingCadence）
  → LeaseTerminated event（forced: false, isRenewal: false, isReplacement: true）
  → LeaseCreated event
  → LeaseReplaced event
```

---

## 附件設計

### 設計原則

附件為純 CRUD 輔助資料，不參與任何 BC 的業務規則或 Aggregate 狀態機。生命週期跟隨宿主資源（宿主軟刪除時，應用層於同一 DB transaction 內同步軟刪除其附件）。

### 支援附件的資源

| 資源 | 附件表 | 備註 |
|------|--------|------|
| Property | `property_attachments` | 物業照片 |
| Room | `room_attachments` | 房間照片 |
| Tenant | `tenant_attachments` | 身份文件等；沿用既有 tenant flow 的 property access 規則 |
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
  Body: { resource_type, resource_id, file_name, content_type, file_size }
  → 後端驗證呼叫者對 resource 有寫入權限
  → 後端驗證 BR-18 MIME type 與 file_size 不超過 20MB
  → 後端建立暫存 token（nonce, object_path, issued_to, resource_type, resource_id, expires_at）
  → 回傳 { upload_url, nonce, expires_at }（Signed URL 含鎖定 content_type）

Step 2：Client 直接 PUT 到 GCS（不經後端）

Step 3：POST /{resource}/{id}/attachments
  Body: { nonce, file_name }
  → 後端以 nonce 驗證合法性（issued_to、resource_id 需一致）
  → 後端呼叫 GCS HEAD 確認物件存在，並以 metadata 再確認 content_type 與 size
  → 寫入對應 attachment table，刪除已用 nonce
```

Tenant attachment endpoints use the same tenant access model as tenant detail and lease-history flows. The tenant must exist, and non-admin management roles must have access to one of the tenant's associated properties.

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

**部署前置條件**：GCS bucket 需設定 CORS policy（允許前端 origin，methods: PUT，headers: Content-Type）；後端 Service Account 需具備產生 signed URL、`storage.objects.create`、`storage.objects.get` 權限。後端設定需提供 `GCS_BUCKET_NAME`、`GCS_SIGNED_URL_TTL`，本地 emulator 可使用 `STORAGE_EMULATOR_HOST`。一般 automated tests 應注入 fake storage port，不依賴真 GCS。

---

## 排程任務

| 任務 | 頻率 | 說明 |
|------|------|------|
| 逾期帳單掃描 | 每日凌晨 | 掃描 `due_date < today AND status = pending_payment AND deleted_at IS NULL`，批次更新為 `overdue`。與付款衝突時樂觀鎖失敗，跳過該筆。 |
| 逾期帳單催收通知 | 每週一次 | 掃描 `status = overdue AND deleted_at IS NULL`，對租客寄催收 Email，每張帳單最多寄 3 次（記錄 `overdue_notice_count`）。 |
| 租約到期掃描 | 每日凌晨 | 掃描 `end_date < today AND status = active AND deleted_at IS NULL`，批次更新 Lease status 為 `expired`，Room 維持 `occupied`，不自動終止。 |
| 租約到期提醒 | 每日凌晨 | 掃描 `end_date = today + 30 days AND status = active`，寄送提醒 Email 給主辦和員工。 |
| 月結快照產出 | 每月最後一天 23:59 | 將 PropertyAccount 當月 AccountingEntry 封存為 `monthly_snapshot_entries`，排除軟刪除物業（`deleted_at IS NULL`），清空當月暫存資料。 |
| 強制終止補償 | 已棄用 | 舊設計掃描 `force_terminations WHERE status = in_progress` 續行 written_off；目前強制終止 command 同步完成 write-off 與 ForceTermination completion。 |
| Upload token 清理 | 每日凌晨 | 清除 `attachment_upload_tokens WHERE expires_at < now`。 |
| GCS 附件清理 | 每日凌晨 | 掃描 `*_attachments WHERE deleted_at < now - INTERVAL '90 days'`，刪除對應 GCS 物件後硬刪除 DB 記錄。 |

---

## 設計決策紀錄（ADR）

| 決策 | 選擇 | 理由 |
|------|------|------|
| 押金建模 | Lease 內的 Value Object | 押金生命週期完全跟著租約，不需獨立 Aggregate |
| 續約處理 | Lease replacement API | 續約只是 replacement 的其中一種 reason，不單獨定義 LeaseRenewed |
| 電費帳單 cadence | Property 定義預設值，Lease 建立時決定有效值 | 支援 monthly / bimonthly；既有 Lease 不受 Property 預設值更新影響 |
| 電費帳單建立 | 租約建立時依 Lease cadence 一併預產，初始 pending_meter | 與租金帳單同批預產，Bill 補 periodStart/periodEnd，MeterRecorded 時找對應 period 更新金額 |
| 租金調整後帳單 | void `due_date >= nextPaymentDate` 的帳單，從 nextPaymentDate 重產 | 當月帳單不動，業務語意清楚（這個月金額已說好）；nextPaymentDate 計算複用 BR-15 邏輯 |
| 付款日 | 固定為租約起始日，不可單獨修改，特殊月份順延月底 | 消除帳單產生邏輯的邊界案例，paymentDay 欄位移除 |
| JournalEntry 拆分 | JournalLog + RepairRequest | 兩者行為差異過大，合併導致 schema nullable 欄位過多 |
| PropertyAccount 架構 | 月結快照：Aggregate 只持有當月，歷史走 MonthlySnapshot | 解決 Aggregate 無限增長問題，寫入效能穩定 |
| 租客角色 | 無系統帳號，Email 通知 | 降低系統複雜度，租客透過 Email 收帳單 |
| 代管費 | 超出範圍 | 工作室財務另外處理，不在 STDS 範圍內 |
| 跨 BC 傳遞 | In-process event bus + command-owned direct orchestration | 單一服務架構，無需 message queue；需要強一致性的 side effect 留在 command transaction |
| 軟刪除 | 全部軟刪除，所有查詢加 deleted_at IS NULL filter | 歷史帳單和日誌需保留關聯，不可真刪除 |
| 強制終止租約 | 同步完成 ForceTermination，保留已棄用補償式 `in_progress` 語意作歷史參考 | 目前事件匯流排為 in-process，強制終止 command 在交易內同步完成帳單 write-off、`deposit_handling` 記錄與 `completed` 狀態，避免引入尚未需要的排程補償流程 |
| Room 競態防護 | 建立租約時對 Room 取悲觀鎖（select for update）再檢查 BR-12 | 防止兩個請求同時對同一 Room 建立租約 |
| maintenance → vacant 條件 | 該 Room 所有 RepairRequest 均 completed 或 cancelled 才轉回 vacant | 多個 RepairRequest 場景下避免過早開放房間；未來實作應走 repair workflow direct orchestration |
| BR-09 移除 | 移除，BR-04 已完整涵蓋 | 退租流程統一由 BR-04 把關，不重複檢查 |
| 物業指派移除 event | 舊設計曾預留 PropertyUnassigned event，進行中操作不撤銷 | 目前 runtime 沒有此 event struct；下次操作時權限自然擋住，不需要複雜的操作撤銷邏輯 |
| BR-08 維修中刪除 | 移除例外，維修中房間不得刪除；maintenance 狀態由 active RepairRequest 維持 | 刪除維修中 Room 導致 RepairRequest 狀態機孤立 |
| 逾期競態 | 樂觀鎖，付款優先，批次跳過衝突 | 帳單已付款則批次自然不再掃到，無需額外處理 |
| overdue → paid | 允許 | 逾期帳單仍應可收款，不因逾期狀態阻斷收款流程 |
| 押金部分扣款 | Deposit VO 拆分 deductionAmount + refundAmount，status 改為 settled 取代 refunded/deducted | 台灣退租最常見情境是「扣一部分、退餘額」，原 refunded/deducted 二選一無法表達；DepositDeducted + DepositRefunded 兩事件可依序發出，但 PropertyAccount 分錄應由押金處理 command direct accounting 寫入 |
| Lease replacement 通知語意 | LeaseTerminated 加 `isReplacement: bool`，並新增 LeaseReplaced event | Notification BC 對 replacement 不寄退租確認；LeaseReplaced 目前作為審計 / future notification candidate |
| 認證機制 | Firebase Auth + Custom Claims | 降低自建 JWT 與密碼管理複雜度；Custom Claims 支援 server-side 更新，可在物業指派變更後立即同步 role 與 assigned_property_ids 至 token；users table 改存 firebase_uid 取代 password_hash |
| 附件業務定位 | 純 CRUD，不參與業務規則 | 附件為輔助資訊，不影響任何 Aggregate 狀態機或業務決策；保持模型簡潔，避免過度設計 |
| 附件表架構 | 方案 B：各資源獨立附件表 | 維持真實 FK 約束，符合現有 BC 邊界設計風格；軟刪除 cascade 由應用層 transaction 保證 |
| 附件上傳機制 | Signed URL + nonce 綁定 | Client 直傳 GCS 減少後端 I/O；nonce 機制防止任意 object_path 注入；Step 3 GCS HEAD 確認物件存在 |
| GCS 部署依賴 | CORS 與 IAM 為部署前置條件（accepted risk） | Signed URL 直傳需要瀏覽器 CORS 支援；Service Account 權限不足會導致 URL 產生失敗；兩者屬基礎設施設定，在首次部署前完成 |
