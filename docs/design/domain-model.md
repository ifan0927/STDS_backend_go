# Domain Model

狀態：current as-is model，驗證日期 2026-07-10。

本文描述目前 bounded contexts、aggregates、states、lifecycles 與 cross-BC boundaries。它不定義新的 business rules；已確認規則見 [`domain-rule.md`](domain-rule.md)，未確認或具風險的議題見 [`decision-backlog.md`](decision-backlog.md)，技術取捨見 [`decisions/`](decisions/)。

## Bounded Contexts

| Bounded context | 核心責任 | Current status |
| --- | --- | --- |
| Identity & Access | Firebase identity對應、DB user、role與property scope | implemented；`permission_overrides`語意 unresolved |
| Property | Property、Room、occupancy、maintenance與dashboard read models | implemented；lease lifecycle的post-commit occupancy仍有一致性風險 |
| Leasing | Tenant、Lease、cadence、replacement、termination與checkout settlement | implemented；co-tenant、early termination future bills、replacement meter baseline unresolved |
| Billing | Bill、meter、payment、accounting、snapshot與financial reports | implemented；historical accounting mapping與post-close amendment unresolved |
| Journal | JournalLog、expense與RepairRequest workflow | implemented；repair complete/cancel會在同一transaction處理room recovery |
| Attachment | Upload authorization、metadata registration、resource access | runtime implemented；retention/hard-delete/legacy mapping unresolved |
| Notification | Email adapter、onboarding與scheduler通知 | partial；多個events只有trace/reserved，非delivery承諾 |
| Brand Public Read | Approved-view based public profile/FAQ/availability | implemented |

STDS目前是單一Go service。Bounded context是邏輯與ownership邊界，不代表獨立部署。

## Identity And Access

### User

- Firebase Auth管理authentication；DB `users` record透過`firebase_uid`連結外部身份。
- Request authorization principal由middleware載入DB user後建立；role與property assignments以DB值為runtime source of truth。
- Roles目前為admin、organizer、staff、owner。
- Property access由router policy、resource/property resolver與authorization middleware共同執行。
- Firebase Custom Claims讀寫能力仍存在，但不是runtime authorization authority。
- `permission_overrides`有storage/API representation，正式allow/deny與priority語意尚未確認。

## Property

### Property

- 保存owner、名稱、地址、電價與default electricity cadence等現行狀態。
- Property建立時，PropertyAccount在同一transaction建立。
- Public brand availability透過approved database view暴露，不允許public handler直接讀取敏感base tables。

### Room

- States：`vacant | occupied | maintenance`。
- 建立lease後，runtime以post-commit `LeaseCreated` subscriber改為occupied。
- 終止lease後，runtime以post-commit `LeaseTerminated` subscriber改為vacant。
- 設為maintenance時，room狀態與room-scoped repair request在同一transaction建立。
- Repair complete/cancel會在同一transaction執行conditional room recovery；仍有其他active repair時維持maintenance。對應event只保留trace。

Room/tenant lease side effects已實作，但post-commit失敗無durable retry，因此屬已知一致性風險，不是原子保證。

## Leasing

### Tenant

- Tenant與Lease分開保存，tenant可跨多份lease重用。
- States：`active | inactive`。
- 新lease的post-commit subscriber可把tenant設為active；lease終止後另一subscriber在沒有active/expired lease時設為inactive。
- 自然人identity、legacy dedupe與共同承租人仍未定義。

### Lease

- States：`active | expired | terminated | force_terminated`。
- 保存rent/electricity billing cadences、deposit、start/end dates、optional actual move-out date、starting meter與settlement detail。
- Create flow會在transaction內建立lease並預產rent/electricity bills，再於commit後發布`LeaseCreated`。
- Update flow只支援現行contract允許的租金條件調整；cadence改變使用replacement flow。
- Replacement flow在同一command中終止舊lease、建立新lease與bills，再發布termination/creation events。Meter baseline仍是decision backlog。
- Normal termination/checkout finalize在transaction內完成核心lease、deposit、accounting與settlement state，再發布`LeaseTerminated`。
- Force termination有獨立record與bill write-off流程；允許write off的精確bill集合尚未成為production rule。

### Checkout Settlement

- Preview是read/compute operation，不改變state；結果包含blockers、warnings、lines、totals與token。
- Finalize會在lock/transaction boundary內重算並驗token，保存settlement snapshot、處理deposit/accounting並終止lease。
- Completed export從persisted settlement snapshot render，不從mutable joins重建。
- Reopen/amendment尚未實作或確認。

## Billing

### Bill

- Types：`rent | electricity`。
- Rent lifecycle包含`pending_payment`、`overdue`、`paid`、`voided`與`written_off`。
- Electricity lifecycle從`pending_meter`進入付款狀態，並可進入`overdue`、`paid`、`voided`或`written_off`。
- Meter command在同一transaction計算usage/amount並更新bill；`MeterRecorded`只保留trace/reserved用途。
- Payment command在同一transaction更新bill並建立AccountingEntry；`BillPaid`不負責補寫accounting。
- Overdue scanning與reminder由scheduler use cases處理。

### PropertyAccount And Reports

- PropertyAccount保存當期AccountingEntry；來源包含bill payment、journal expense、deposit/refund/deduction與checkout effects。
- Monthly snapshot保存finalized entries與當時的accounting title/display facts。
- Current report組合當期entries與historical snapshots；HTML exports由backend render。
- 月結後補帳、重開與amendment尚未確認。

## Journal And Repair

### JournalLog

- 保存文字記錄與optional expense。
- Expense與AccountingEntry在journal command的同一transaction建立。
- `JournalExpenseRecorded`只作trace/reserved event。

### RepairRequest

- States：`submitted -> assigned -> in_progress -> completed`，active states也可轉`cancelled`。
- Assignment對象是system user；vendor coordination不在現行data model。
- Complete/cancel在同一transaction執行room state recovery；只有沒有其他active repair時恢復vacant。

## Attachments

- 支援property、room、tenant、lease、bill、journal與repair等現行resource associations。
- Flow分為upload authorization、object upload、metadata verification/registration與authorized read/download。
- Runtime contract不等於retention policy；ID影本、hard-delete與legacy objects仍是decision backlog。

## Domain Event Boundary

目前event bus是synchronous、in-process、post-commit dispatcher：

1. Application command在DB transaction中record events。
2. Transaction commit成功後，tx runner依序publish。
3. Subscriber failure會回傳error，但不能rollback已commit的command。
4. 沒有outbox、retry queue、dead-letter queue或durable delivery guarantee。

需要與command強一致的財務或核心狀態必須在command transaction內direct orchestration。只有允許commit後失敗且不需rollback的follow-up才適合現行subscriber。完整決策見 [`decisions/0002-event-boundary.md`](decisions/0002-event-boundary.md)。

## Cross-Context Boundary Matrix

| Side effect | Current path | Classification |
| --- | --- | --- |
| Lease create/terminate -> room/tenant state | post-commit subscribers | implemented, consistency risk |
| Payment -> accounting entry | same command transaction | implemented, strong boundary |
| Journal expense -> accounting entry | same command transaction | implemented, strong boundary |
| Checkout -> settlement/deposit/accounting/lease termination | same command transaction | implemented, strong boundary |
| Property -> PropertyAccount | same command transaction | implemented, strong boundary |
| Room maintenance -> RepairRequest | same command transaction | implemented, strong boundary |
| Repair complete/cancel -> conditional room recovery | same command transaction | implemented, strong boundary |
| Receipt/report event -> email delivery | no required subscriber | reserved/not implemented |

## Read Models

Read APIs與reports可以使用query repositories直接組合DTO，不必經aggregate。這是read/write responsibility分離，不允許read model繞過authorization scope，也不把read projection升格成business truth。
