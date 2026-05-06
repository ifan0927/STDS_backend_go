-- ============================================================
-- STDS Database Schema
-- 產出依據：docs/design/domain-model.md v3.3
-- 設計原則：
--   - 全部軟刪除，加 deleted_at 欄位
--   - 金額用整數儲存（台幣，無小數）
--   - 樂觀鎖 aggregate 加 version 欄位
--   - Value Object 展開為多欄位（欄位數少、查詢頻繁）或 JSONB（結構化但不查詢）
--   - 認證由 Firebase Auth 管理，users table 存 firebase_uid，不存 password_hash
-- ============================================================


CREATE EXTENSION IF NOT EXISTS pgcrypto;


-- ============================================================
-- Table: users
-- Aggregate: User Aggregate（Identity & Access BC）
-- 說明: 工作室成員與業主的帳號，包含 firebase_uid、角色、個別權限覆寫、物業指派
--       v3.0：認證改為 Firebase Auth，移除 password_hash，改存 firebase_uid
-- ============================================================

CREATE TABLE users (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- firebase_uid：對應 Firebase Auth 中的使用者 UID，由 Firebase 管理認證
    -- v3.0：取代原本的 password_hash，後端不儲存密碼
    firebase_uid    VARCHAR(128) NOT NULL,
    email           VARCHAR(255) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    -- role 為 Value Object，展開為欄位；RBAC 授權模型基礎
    -- role 與 assigned_property_ids 也透過 Firebase Custom Claims 存於 ID token
    role            VARCHAR(50)  NOT NULL CHECK (role IN ('admin', 'organizer', 'staff', 'owner')),
    -- permission_overrides：個別權限覆寫清單，結構化但不需獨立查詢，用 JSONB
    -- 選擇 JSONB 原因：覆寫清單無需 JOIN 查詢，直接序列化隨 User 讀取即可
    permission_overrides JSONB   NOT NULL DEFAULT '[]',
    -- assigned_property_ids：物業指派清單，透過 Firebase Custom Claims 存於 ID token；用 JSONB array 避免額外 table
    -- 選擇 JSONB 原因：domain model 指出指派清單整批寫入 Custom Claims，不需獨立 JOIN 查詢
    -- 注意：Custom Claims 上限 1000 bytes，接近上限時 middleware 改從 DB 查詢（搭配 cache）
    assigned_property_ids JSONB  NOT NULL DEFAULT '[]',
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version         INTEGER      NOT NULL DEFAULT 1
);

-- Index 說明:
-- idx_users_firebase_uid: 後端 middleware 以 firebase_uid 查詢使用者（每個 request 驗 token 後映射）
CREATE UNIQUE INDEX idx_users_firebase_uid ON users (firebase_uid) WHERE deleted_at IS NULL;
-- idx_users_email: 以 email 查詢使用者（GET /users 搜尋、建立帳號時檢查重複）
CREATE UNIQUE INDEX idx_users_email ON users (email) WHERE deleted_at IS NULL;
-- idx_users_role: GET /users 依 role filter 查詢成員列表
CREATE INDEX idx_users_role ON users (role) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: properties
-- Aggregate: Property Aggregate（Property BC）
-- 說明: 物業基本資料，包含電價設定；Room entities 拆獨立表
-- ============================================================

CREATE TABLE properties (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    VARCHAR(200) NOT NULL,
    address                 TEXT         NOT NULL,
    -- electricityUnitPrice：台幣正數/度，物業層級電價，可接受小數
    electricity_unit_price  NUMERIC(10,4) CHECK (electricity_unit_price > 0),
    default_electricity_billing_cadence VARCHAR(20) NOT NULL DEFAULT 'monthly' CHECK (default_electricity_billing_cadence IN ('monthly', 'bimonthly')),
    -- owner_id：業主帳號，resource-based 存取控制用
    owner_id                UUID         NOT NULL REFERENCES users(id),
    subtitle                VARCHAR(200),
    contact_phone           VARCHAR(50),
    contact_email           VARCHAR(255),
    notes                   TEXT,
    facilities              JSONB,
    deleted_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version                 INTEGER      NOT NULL DEFAULT 1
);

-- Index 說明:
-- idx_properties_owner_id: resource-based 查詢（業主查看自己名下物業）
CREATE INDEX idx_properties_owner_id ON properties (owner_id) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: rooms
-- Aggregate: Property Aggregate（Property BC）—— Room Entity
-- 說明: 房間資料，隸屬於 Property，狀態由 Property Aggregate 統一管理
-- ============================================================

CREATE TABLE rooms (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id         UUID        NOT NULL REFERENCES properties(id),
    name                VARCHAR(100) NOT NULL,
    -- status：vacant | occupied | maintenance
    status              VARCHAR(20)  NOT NULL CHECK (status IN ('vacant', 'occupied', 'maintenance'))
                                     DEFAULT 'vacant',
    size                NUMERIC(10,2) CHECK (size >= 0),
    floor               VARCHAR(50),
    room_type           VARCHAR(50),
    facilities          JSONB,
    default_rent_amount INTEGER CHECK (default_rent_amount > 0),
    notes               TEXT,
    zone                VARCHAR(100),
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
    -- 注意：Room 無獨立 version 欄位，由 Property Aggregate 樂觀鎖管控整體一致性
    -- BR-12 對 Room 取悲觀鎖（SELECT FOR UPDATE），在應用層執行
);

-- Index 說明:
-- idx_rooms_property_id: 查詢物業下所有房間（GET /properties/{id}/rooms）
CREATE INDEX idx_rooms_property_id ON rooms (property_id) WHERE deleted_at IS NULL;
-- idx_rooms_property_status: 篩選物業內特定狀態房間（dashboard、出租率計算）
CREATE INDEX idx_rooms_property_status ON rooms (property_id, status) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: tenants
-- Aggregate: Tenant Aggregate（Leasing BC）
-- 說明: 租客基本資料，長期保留不刪除（軟刪除保留歷史），聯絡方式用 JSONB
-- ============================================================

CREATE TABLE tenants (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    email           VARCHAR(255),
    phone           VARCHAR(50),
    -- contacts：聯絡方式清單（Value Objects），JSONB
    -- 選擇 JSONB 原因：聯絡方式為 Value Object 清單，無需獨立查詢，隨 Tenant 一起載入
    contacts        JSONB         NOT NULL DEFAULT '[]',
    birth_date      DATE,
    national_id     VARCHAR(50),
    address         TEXT,
    occupation      VARCHAR(100),
    -- status：active | inactive；有效租約時 active
    status          VARCHAR(20)   NOT NULL CHECK (status IN ('active', 'inactive'))
                                  DEFAULT 'active',
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    version         INTEGER       NOT NULL DEFAULT 1
);

-- Index 說明:
-- idx_tenants_status: Leasing BC Application Service 查詢 tenant status（LeaseTerminated 後判斷）
CREATE INDEX idx_tenants_status ON tenants (status) WHERE deleted_at IS NULL;
-- idx_tenants_email: 查詢租客（避免重複建立）
CREATE INDEX idx_tenants_email ON tenants (email) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: leases
-- Aggregate: Lease Aggregate（Leasing BC）
-- 說明: 租約，包含押金（deposit）展開欄位；帳單為獨立 Aggregate 以外鍵關聯
-- ============================================================

CREATE TABLE leases (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID        NOT NULL REFERENCES tenants(id),
    room_id                 UUID        NOT NULL REFERENCES rooms(id),
    property_id             UUID        NOT NULL REFERENCES properties(id),
    -- 租約條件
    rent_amount             INTEGER      NOT NULL CHECK (rent_amount > 0),  -- BR-02
    start_date              DATE         NOT NULL,
    end_date                DATE         NOT NULL,
    rent_billing_cadence     VARCHAR(20)  NOT NULL DEFAULT 'monthly' CHECK (rent_billing_cadence IN ('monthly', 'quarterly', 'semiannual', 'annual')),
    electricity_billing_cadence VARCHAR(20) NOT NULL DEFAULT 'monthly' CHECK (electricity_billing_cadence IN ('monthly', 'bimonthly')),
    -- status：active | expired | terminated | force_terminated
    status                  VARCHAR(30)  NOT NULL CHECK (status IN ('active', 'expired', 'terminated', 'force_terminated'))
                                         DEFAULT 'active',
    -- deposit 為 Value Object，展開為欄位
    -- v3.2：支援部分扣款後退餘額，status 改為 held | settled | written_off
    deposit_amount          INTEGER      NOT NULL CHECK (deposit_amount >= 0),  -- BR-03
    deposit_refund_amount   INTEGER CHECK (deposit_refund_amount >= 0),
    deposit_deduction_amount INTEGER CHECK (deposit_deduction_amount >= 0),
    deposit_status          VARCHAR(20)  NOT NULL CHECK (deposit_status IN ('held', 'settled', 'written_off'))
                                         DEFAULT 'held',
    deposit_deduction_reason TEXT,  -- BR-10 押金扣款須填寫原因
    notes                   TEXT,
    termination_reason      TEXT,
    settlement_detail       JSONB,
    deleted_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version                 INTEGER      NOT NULL DEFAULT 1
);

-- Index 說明:
-- idx_leases_property_status: 查詢物業下租約（GET /leases?propertyId=...&status=...）
CREATE INDEX idx_leases_property_status ON leases (property_id, status) WHERE deleted_at IS NULL;
-- idx_leases_tenant_id: 查詢租客的租約歷史（GET /tenants/{id}/leases）
CREATE INDEX idx_leases_tenant_id ON leases (tenant_id) WHERE deleted_at IS NULL;
-- idx_leases_room_id: 外鍵關聯，查詢房間的租約
CREATE INDEX idx_leases_room_id ON leases (room_id) WHERE deleted_at IS NULL;
-- idx_leases_end_date_status: 租約到期掃描排程（end_date < today AND status = active）
CREATE INDEX idx_leases_end_date_status ON leases (end_date, status) WHERE deleted_at IS NULL;
-- idx_leases_status: 一般狀態篩選
CREATE INDEX idx_leases_status ON leases (status) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: bills
-- Aggregate: Bill Aggregate（Billing BC）
-- 說明: 帳單主表，涵蓋租金帳單與電費帳單；付款紀錄與 MeterReading 展開為欄位
-- ============================================================

CREATE TABLE bills (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id            UUID        NOT NULL REFERENCES leases(id),
    -- tenant_id 從 Lease 取得直接存入，支援 (tenant_id, status) 查詢 index（domain model 明確指定）
    tenant_id           UUID        NOT NULL REFERENCES tenants(id),
    -- room_id 從 Lease 取得直接存入，電表查詢不需 JOIN（domain model 明確指定）
    room_id             UUID        NOT NULL REFERENCES rooms(id),
    property_id         UUID        NOT NULL REFERENCES properties(id),
    -- type：rent | electricity
    type                VARCHAR(20)  NOT NULL CHECK (type IN ('rent', 'electricity')),
    amount              INTEGER,     -- 電費帳單預產時為 null，抄表後依 usage × unitPrice 四捨五入填入
    period_start        DATE         NOT NULL,
    period_end          DATE         NOT NULL,
    due_date            DATE         NOT NULL,
    -- status：
    --   租金帳單：pending_payment → paid | overdue | voided | written_off
    --   電費帳單：pending_meter → pending_payment → paid | overdue | voided | written_off
    status              VARCHAR(30)  NOT NULL CHECK (status IN (
                            'pending_meter', 'pending_payment', 'paid',
                            'overdue', 'voided', 'written_off'
                        )),
    -- 付款紀錄（Value Object），展開為欄位
    -- 選擇展開原因：欄位少（3 個），收款確認後一次寫入，不需 JSONB 彈性
    payment_method      VARCHAR(20)  CHECK (payment_method IN ('cash', 'transfer', 'other')),
    paid_at             TIMESTAMPTZ,
    paid_amount         INTEGER,
    -- MeterReading（Value Object，電費帳單專用），展開為欄位
    -- 選擇展開原因：各欄位獨立用於計算與查詢（previousReading 由系統查詢填入）
    meter_previous_reading  INTEGER,
    meter_current_reading   INTEGER,
    meter_unit_price        NUMERIC(10,4), -- 抄表當下從 Property 取得並鎖定，台幣正數，可接受小數
    meter_recorded_at       TIMESTAMPTZ,
    -- written_off 時的原因（強制終止使用）
    written_off_reason  TEXT,
    -- 逾期催收通知已寄次數，上限 3（BR 於排程任務層控制）
    overdue_notice_count INTEGER      NOT NULL DEFAULT 0,
    -- sourceRef 預留欄位（現階段不實作）
    source_ref          JSONB,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version             INTEGER      NOT NULL DEFAULT 1,
    CONSTRAINT bills_period_range_check CHECK (period_start <= period_end)
);

-- Index 說明:
-- idx_bills_property_status_due_date: 主要查詢 index（帳單列表、逾期掃描排程）
CREATE INDEX idx_bills_property_status_due_date ON bills (property_id, status, due_date) WHERE deleted_at IS NULL;
-- idx_bills_lease_id: 查詢租約下所有帳單
CREATE INDEX idx_bills_lease_id ON bills (lease_id) WHERE deleted_at IS NULL;
-- idx_bills_tenant_status: 租客帳單查詢（GET /bills?tenantId=...&status=...）
CREATE INDEX idx_bills_tenant_status ON bills (tenant_id, status) WHERE deleted_at IS NULL;
-- idx_bills_status_due_date: 逾期帳單掃描排程（due_date < today AND status = pending_payment）
CREATE INDEX idx_bills_status_due_date ON bills (status, due_date) WHERE deleted_at IS NULL;
-- idx_bills_property_type_due_date: 電表相關查詢（GET /properties/{id}/pending-meter, /meter-history）
CREATE INDEX idx_bills_property_type_due_date ON bills (property_id, type, due_date) WHERE deleted_at IS NULL;
-- idx_bills_room_type_due_date: 單房間電表歷史（GET /rooms/{id}/meter-history）
CREATE INDEX idx_bills_room_type_due_date ON bills (room_id, type, due_date) WHERE deleted_at IS NULL;
-- idx_bills_status_overdue_notice_count: 逾期催收通知排程（status = overdue AND overdue_notice_count < 3）
CREATE INDEX idx_bills_status_overdue_notice_count ON bills (status, overdue_notice_count) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: property_accounts
-- Aggregate: PropertyAccount Aggregate（Billing BC）
-- 說明: 每個物業一個帳戶，持有當月未結算的 AccountingEntry（歷史走 monthly_snapshots）
-- ============================================================

CREATE TABLE property_accounts (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID        NOT NULL UNIQUE REFERENCES properties(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    version     INTEGER      NOT NULL DEFAULT 1
);

-- Index 說明:
-- idx_property_accounts_property_id: 透過 property_id 查詢帳戶（BillPaid 後寫入 AccountingEntry）
CREATE UNIQUE INDEX idx_property_accounts_property_id ON property_accounts (property_id) WHERE deleted_at IS NULL;


-- ============================================================
-- Table: accounting_entries
-- Aggregate: PropertyAccount Aggregate（Billing BC）—— AccountingEntry Entity
-- 說明: 當月暫存的會計分錄；月結後封存至 monthly_snapshot_entries
-- ============================================================

CREATE TABLE accounting_entries (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_account_id UUID        NOT NULL REFERENCES property_accounts(id),
    -- category：財報分類
    category            VARCHAR(50)  NOT NULL CHECK (category IN (
                            'rent_payment', 'electricity_payment',
                            'deposit_refund', 'deposit_deduction', 'journal_expense'
                        )),
    amount              INTEGER      NOT NULL,
    description         TEXT,
    -- source_ref：來源事件與 ID（BillPaid / JournalExpenseRecorded / DepositRefunded / DepositDeducted）
    source_ref          JSONB        NOT NULL,
    year                SMALLINT     NOT NULL,
    month               SMALLINT     NOT NULL CHECK (month BETWEEN 1 AND 12),
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_accounting_entries_account_year_month: 讀取當月資料（財報組合查詢）
CREATE INDEX idx_accounting_entries_account_year_month ON accounting_entries (property_account_id, year, month);


-- ============================================================
-- Table: monthly_snapshots
-- 說明: 月結快照摘要（summary 層），對應 domain model 的 monthly_snapshots table
-- ============================================================

CREATE TABLE monthly_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id     UUID        NOT NULL REFERENCES properties(id),
    year            SMALLINT     NOT NULL,
    month           SMALLINT     NOT NULL CHECK (month BETWEEN 1 AND 12),
    total_income    INTEGER      NOT NULL DEFAULT 0,
    total_expense   INTEGER      NOT NULL DEFAULT 0,
    net             INTEGER      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (property_id, year, month)
);

-- Index 說明:
-- idx_monthly_snapshots_property_year_month: 財報歷史查詢（GET /properties/{id}/financial-report）
CREATE UNIQUE INDEX idx_monthly_snapshots_property_year_month ON monthly_snapshots (property_id, year, month);


-- ============================================================
-- Table: monthly_snapshot_entries
-- 說明: 月結快照明細（entries 層），對應 domain model 的 monthly_snapshot_entries table
-- ============================================================

CREATE TABLE monthly_snapshot_entries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id     UUID        NOT NULL REFERENCES monthly_snapshots(id),
    category        VARCHAR(50)  NOT NULL CHECK (category IN (
                        'rent_payment', 'electricity_payment',
                        'deposit_refund', 'deposit_deduction', 'journal_expense'
                    )),
    description     TEXT,
    amount          INTEGER      NOT NULL,
    source_ref      JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_monthly_snapshot_entries_snapshot_category: 按分類查詢月結明細（財報詳情）
CREATE INDEX idx_monthly_snapshot_entries_snapshot_category ON monthly_snapshot_entries (snapshot_id, category);


-- ============================================================
-- Table: journal_logs
-- Aggregate: JournalLog Aggregate（Journal BC）
-- 說明: 物業日誌，可含費用條目；無狀態機
-- ============================================================

CREATE TABLE journal_logs (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID        NOT NULL REFERENCES properties(id),
    room_id     UUID        REFERENCES rooms(id),  -- optional，日誌可能不關聯特定房間
    author_id   UUID        NOT NULL REFERENCES users(id),
    content     TEXT         NOT NULL,
    -- expense 為 optional Value Object，展開為欄位
    -- 選擇展開原因：費用資料結構簡單（金額 + 描述），展開後可直接彙總
    expense_amount      INTEGER,        -- null 表示無費用
    expense_description TEXT,
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_journal_logs_property_created_at: 查詢物業日誌列表（GET /journal-logs?propertyId=...），含排序
CREATE INDEX idx_journal_logs_property_created_at ON journal_logs (property_id, created_at DESC) WHERE deleted_at IS NULL;
-- idx_journal_logs_room_id: 外鍵關聯
CREATE INDEX idx_journal_logs_room_id ON journal_logs (room_id) WHERE deleted_at IS NULL AND room_id IS NOT NULL;


-- ============================================================
-- Table: repair_requests
-- Aggregate: RepairRequest Aggregate（Journal BC）
-- 說明: 報修派工，有完整狀態機
-- ============================================================

CREATE TABLE repair_requests (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id     UUID        NOT NULL REFERENCES properties(id),
    room_id         UUID        NOT NULL REFERENCES rooms(id),
    submitted_by    UUID        NOT NULL REFERENCES users(id),
    assigned_to     UUID        REFERENCES users(id),  -- nullable
    title           VARCHAR(200) NOT NULL,
    description     TEXT         NOT NULL,
    cancel_reason   TEXT,
    -- status：submitted | assigned | in_progress | completed | cancelled
    status          VARCHAR(20)  NOT NULL CHECK (status IN (
                        'submitted', 'assigned', 'in_progress', 'completed', 'cancelled'
                    )) DEFAULT 'submitted',
    submitted_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    assigned_at     TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_repair_requests_property_status: 查詢物業維修列表（GET /repair-requests?propertyId=...&status=...）
CREATE INDEX idx_repair_requests_property_status ON repair_requests (property_id, status) WHERE deleted_at IS NULL;
-- idx_repair_requests_room_id: 外鍵關聯，Property BC 訂閱 RepairCompleted/Cancelled 後檢查同房間所有 RepairRequest
CREATE INDEX idx_repair_requests_room_id ON repair_requests (room_id) WHERE deleted_at IS NULL;
-- idx_repair_requests_assigned_to: 外鍵關聯
CREATE INDEX idx_repair_requests_assigned_to ON repair_requests (assigned_to) WHERE deleted_at IS NULL AND assigned_to IS NOT NULL;


-- ============================================================
-- Table: scheduler_job_runs
-- 說明: 排程任務執行紀錄，用於 job window 去重與重試追蹤
-- ============================================================

CREATE TABLE scheduler_job_runs (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    job_key     VARCHAR(100) NOT NULL,
    window_key  VARCHAR(100) NOT NULL,
    request_id  VARCHAR(100),
    retry_count INTEGER      NOT NULL DEFAULT 0,
    status      VARCHAR(20)  NOT NULL CHECK (status IN ('started', 'completed', 'failed', 'skipped')),
    message     TEXT,
    started_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (job_key, window_key)
);

CREATE INDEX idx_scheduler_job_runs_job_window ON scheduler_job_runs (job_key, window_key);


-- ============================================================
-- Table: force_terminations
-- 說明: 強制終止租約的同步完成記錄
--       對應 domain model 的 force_terminations table
-- ============================================================

CREATE TABLE force_terminations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id        UUID        NOT NULL REFERENCES leases(id),
    initiated_by    UUID        NOT NULL REFERENCES users(id),
    reason          TEXT         NOT NULL,
    deposit_handling VARCHAR(20) NOT NULL CHECK (deposit_handling IN ('write_off', 'keep_held')),
    -- status：completed；in_progress 保留為已棄用的歷史狀態
    status          VARCHAR(20)  NOT NULL CHECK (status IN ('in_progress', 'completed'))
                                 DEFAULT 'in_progress',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_force_terminations_status: 狀態查詢索引；in_progress 為已棄用的歷史狀態
CREATE INDEX idx_force_terminations_status ON force_terminations (status);
-- idx_force_terminations_lease_id: 外鍵關聯
CREATE INDEX idx_force_terminations_lease_id ON force_terminations (lease_id);


-- ============================================================
-- Table: force_termination_bills
-- 說明: 強制終止與帳單的中介表，記錄每筆帳單的 written_off 進度
--       對應 domain model 的 force_termination_bills table
-- ============================================================

CREATE TABLE force_termination_bills (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    force_termination_id    UUID        NOT NULL REFERENCES force_terminations(id),
    bill_id                 UUID        NOT NULL REFERENCES bills(id),
    -- status：pending | done
    status                  VARCHAR(20)  NOT NULL CHECK (status IN ('pending', 'done'))
                                         DEFAULT 'pending',
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_force_termination_bills_ft_status: 補償排程查詢未完成的帳單（WHERE force_termination_id = ? AND status = 'pending'）
CREATE INDEX idx_force_termination_bills_ft_status ON force_termination_bills (force_termination_id, status);
-- idx_force_termination_bills_bill_id: 外鍵關聯
CREATE INDEX idx_force_termination_bills_bill_id ON force_termination_bills (bill_id);


-- ============================================================
-- Table: attachment_upload_tokens
-- 說明: Signed URL 上傳流程的暫存 token
-- ============================================================

CREATE TABLE attachment_upload_tokens (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce           VARCHAR(255) NOT NULL UNIQUE,
    object_path     TEXT         NOT NULL,
    issued_to       UUID         NOT NULL REFERENCES users(id),
    resource_type   VARCHAR(50)  NOT NULL,
    resource_id     UUID         NOT NULL,
    expires_at      TIMESTAMPTZ  NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Index 說明:
-- idx_attachment_upload_tokens_expires_at: Upload token 清理排程
CREATE INDEX idx_attachment_upload_tokens_expires_at ON attachment_upload_tokens (expires_at);
-- idx_attachment_upload_tokens_resource: nonce 驗證後比對 issued resource
CREATE INDEX idx_attachment_upload_tokens_resource ON attachment_upload_tokens (resource_type, resource_id);


-- ============================================================
-- Attachment tables
-- 說明: 各資源獨立附件表，維持真實 FK 約束
-- ============================================================

CREATE TABLE property_attachments (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID        NOT NULL REFERENCES properties(id),
    object_path TEXT        NOT NULL,
    file_name   TEXT        NOT NULL,
    uploaded_by UUID        REFERENCES users(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_property_attachments_property_id ON property_attachments (property_id) WHERE deleted_at IS NULL;

CREATE TABLE room_attachments (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     UUID        NOT NULL REFERENCES rooms(id),
    object_path TEXT        NOT NULL,
    file_name   TEXT        NOT NULL,
    uploaded_by UUID        REFERENCES users(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_room_attachments_room_id ON room_attachments (room_id) WHERE deleted_at IS NULL;

CREATE TABLE tenant_attachments (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id),
    object_path TEXT        NOT NULL,
    file_name   TEXT        NOT NULL,
    uploaded_by UUID        REFERENCES users(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tenant_attachments_tenant_id ON tenant_attachments (tenant_id) WHERE deleted_at IS NULL;

CREATE TABLE lease_attachments (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id    UUID        NOT NULL REFERENCES leases(id),
    object_path TEXT        NOT NULL,
    file_name   TEXT        NOT NULL,
    uploaded_by UUID        REFERENCES users(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lease_attachments_lease_id ON lease_attachments (lease_id) WHERE deleted_at IS NULL;

CREATE TABLE journal_log_attachments (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_log_id UUID        NOT NULL REFERENCES journal_logs(id),
    object_path    TEXT        NOT NULL,
    file_name      TEXT        NOT NULL,
    uploaded_by    UUID        REFERENCES users(id),
    deleted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_journal_log_attachments_journal_log_id ON journal_log_attachments (journal_log_id) WHERE deleted_at IS NULL;

CREATE TABLE repair_request_attachments (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    repair_request_id UUID        NOT NULL REFERENCES repair_requests(id),
    object_path       TEXT        NOT NULL,
    file_name         TEXT        NOT NULL,
    uploaded_by       UUID        REFERENCES users(id),
    sort_order        INTEGER     NOT NULL DEFAULT 0,
    photo_stage       VARCHAR(20) CHECK (photo_stage IN ('before', 'after', 'other')),
    deleted_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_repair_request_attachments_repair_request_id ON repair_request_attachments (repair_request_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_repair_request_attachments_repair_request_sort_order ON repair_request_attachments (repair_request_id, sort_order) WHERE deleted_at IS NULL;

CREATE TABLE bill_attachments (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    bill_id     UUID        NOT NULL REFERENCES bills(id),
    object_path TEXT        NOT NULL,
    file_name   TEXT        NOT NULL,
    uploaded_by UUID        REFERENCES users(id),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bill_attachments_bill_id ON bill_attachments (bill_id) WHERE deleted_at IS NULL;


-- ============================================================
-- RBAC 支援表（混合型授權：RBAC 基礎功能控管）
-- 說明: 混合型授權模型，role 欄位在 users table 中已定義（RBAC 基礎）；
--       resource-based 控制透過 users.assigned_property_ids 與 properties.owner_id 實作。
--       認證由 Firebase Auth 管理，role 與 assigned_property_ids 透過 Firebase Custom Claims 存於 ID token。
--       後端 middleware 以 Firebase Admin SDK 驗證 token 後直接讀取 claims，不需每次查詢 DB。
--       現階段 role 已內建於 users.role 欄位，權限在應用層硬編碼，不建立 role_permissions 表。
-- ============================================================
