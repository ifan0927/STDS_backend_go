# Tech Stack

產出日期：2026-04-15
基於：domain-model.md v3.0 + spec 文件 + 使用者情境

## 情境摘要

10 人內部工具，同時上線人數極低（個位數）。1 人開發，後端 Go + Gin，前端 React + Ant Design + Vite（AI 輔助）。部署全採 GCP，預算上限 1500 TWD/月（約 $46 USD）。認證已確定使用 Firebase Auth + Custom Claims。系統有 8 個 Aggregate、跨 BC 事件傳遞、6 個排程任務、以及 Email 通知需求。

---

## 選型決策

### 語言 & 框架

**後端**
- **選擇**：Go 1.23 + Gin
- **理由**：團隊熟悉度確定，Gin 輕量適合單體架構，Go 的靜態型別有助於 DDD domain 層的 Business Rule 實作
- **放棄的選項**：無，已確定

**前端**
- **選擇**：React + Ant Design + Vite
- **理由**：團隊已確定，Ant Design 內建 Table/Form 元件適合內部管理工具
- **放棄的選項**：無，已確定

**架構模式**
- **選擇**：單體（Monolith）
- **理由**：10 人工具、1 人開發，微服務只會增加部署複雜度與成本。Go Gin 單一 binary 部署 Cloud Run 即可
- **放棄的選項**：微服務（過度設計）

---

### 資料庫

- **選擇**：Cloud SQL for PostgreSQL — `db-f1-micro`（1 shared vCPU, 0.6 GB RAM）
- **理由**：spec 已確定 PostgreSQL（JSONB、UUID、樂觀鎖、悲觀鎖 SELECT FOR UPDATE）。db-f1-micro 約 $7–10 USD/月，10 人內部工具流量完全可承受
- **設定建議**：asia-east1（台灣）、自動備份每日一次、啟用 `pgcrypto` extension（gen_random_uuid）
- **放棄的選項**：
  - db-g1-small（$25/month，超預算且超出需求）
  - Neon/Supabase（非 GCP 全套）

### Brand Public Read Boundary

- **用途**：core backend 透過獨立 public namespace 提供品牌頁 build-time 需要的 read-only endpoints，不直接暴露 admin brand management endpoints。
- **Core migration 責任**：建立 approved views，例如 `approved_brand_profile_v1`、`approved_brand_faq_items_v1`、`approved_brand_property_availability_v1`。
- **Public endpoint 責任**：只讀 approved views，不直接讀 `properties`、`rooms`、`brand_profiles`、`brand_faq_items`、tenant、lease、bill、deposit、attachment、accounting 等 base tables。
- **Credential 邊界**：第一版使用 core backend runtime DB credential，避免為低流量 build-time brand site 增加額外 readonly credential / service 部署複雜度。approved views 仍是 public data exposure boundary。
- **測試策略**：repo 的 PostgreSQL migration contract test 繼續使用 ephemeral equivalent role 驗證 approved views 可讀、base tables 不可讀；本地 `brand_readonly` helper 保留作為歷史/診斷用途，不是現行 brand site deployment dependency。

### Persistence / Data Access

- **整體策略**：SQL-first；migration、schema、index、constraint 以 SQL 為準，business rule 不下沉至 DB trigger / stored procedure
- **GORM 使用邊界**：
  - 可用於簡單 insert/update/select 與 persistence mapping
  - 不作為架構中心，不依賴 hook、auto preload、隱式 transaction
  - 複雜查詢、報表、scheduler scan、locking query（如 `SELECT ... FOR UPDATE`）可直接使用 raw SQL
- **分層方式**：
  - `model`：大致對應 table，負責 persistence mapping
  - `repository`：對應 use case / aggregate / bounded context，只提供當前業務切片真正需要的方法
  - `query`：服務 read model、報表、scheduler scan，直接回 DTO 或輕量 query object
- **實作原則**：
  - 不採「每個 table 先建完整 CRUD repo」策略
  - persistence model、domain model、API model 明確分離
  - Read API 可直接走 query repository；Write API 走 application service + repository
- **理由**：符合目前 vertical slice 開發方式，也保留 PostgreSQL 在鎖定、聚合查詢、批次處理上的優勢

---

### Auth（認證與授權）

- **認證方式**：Firebase Auth（已確定於 schema）
- **授權實作**：
  - RBAC：Go middleware 驗證 Firebase ID token 後，以 `firebase_uid` 載入 DB user principal，使用 DB role（admin/organizer/staff/owner）判斷
  - Resource-based：middleware 使用 DB user principal 內的 `assigned_property_ids` 驗證請求是否在允許的 property 範圍內；owner 以 `properties.owner_id` 判斷
- **Go 整合**：`firebase.google.com/go/v4` Admin SDK，middleware 驗證 ID Token → 載入 DB user principal → 注入 context
- **理由**：Firebase Auth 負責身份驗證，DB user principal 作為授權 source of truth，避免 Custom Claims 與 DB drift 影響 runtime authorization
- **放棄的選項**：JWT 自建（多餘複雜度，Firebase 已包含）、session-based（Cloud Run 無狀態不適合）

---

### Domain Events

- **選擇**：In-process Event Bus（Go channel / 同步 dispatcher）
- **理由**：所有 BC 在同一個 Go process 內，10 人工具無需跨服務事件。同步 dispatcher 在同一個 transaction 內處理跨 BC 副作用（Room 狀態更新、AccountingEntry 建立），實作最簡單且可靠
- **實作方式**：`DomainEvent` interface + Application Service 層在 transaction commit 後觸發 handlers
- **放棄的選項**：Cloud Pub/Sub（外部 message queue，增加成本與複雜度，此規模不需要）

---

### Email 通知

- **選擇**：Resend
- **理由**：API 介面簡潔，官方 Go SDK 可直接整合，適合目前後端主導的通知流程。現階段 email 量低，足以支撐 onboarding、催收、到期提醒、財報寄送等場景
- **補充**：notification infrastructure 先抽象為 `email` / `line` 兩種 channel；本期只落地 email，LINE 只保留抽象介面
- **放棄的選項**：Firebase Extension "Trigger Email"（依賴 Firestore，與 PostgreSQL 架構不搭）、SendGrid（不再採用）

---

### 部署 & 基礎建設

- **後端容器化**：Docker，multi-stage build（builder: golang:1.23-alpine → runtime: alpine）壓縮 image 大小
- **後端部署**：Cloud Run（asia-east1）
  - 最小實例：0（省錢，冷啟動對內部工具可接受）
  - 最大實例：2（避免意外擴展產生費用）
  - 記憶體：512 MB，CPU：1
- **前端部署**：admin frontend 使用 Firebase Hosting；brand frontend 使用 Cloudflare Pages 靜態部署與 preview。
- **Container Registry**：Artifact Registry（$0.10/GB/月，image 小費用極低）
- **CI/CD**：Cloud Build
  - 免費 tier：120 build-minutes/day，完全足夠
  - 觸發條件：push to `main` branch
  - Pipeline：test → build image → push to Artifact Registry → deploy to Cloud Run
- **排程任務**：Cloud Scheduler（前 3 個 job 免費，第 4–6 個約 $0.30/月）→ 呼叫 Cloud Run HTTP endpoints
  - 排程邏輯仍實作於本 backend repo，由 application service / repository 負責執行
  - 觸發方式採外部 scheduler 呼叫受保護 endpoint，不在 app 內維護常駐 cron，也不以 CLI job 為主要模式
  - job endpoint 必須支援 idempotency、鎖定策略與失敗重跑

**預估月費（asia-east1）**

| 服務 | 費用估計 |
|------|---------|
| Cloud SQL db-f1-micro | ~$8–10 USD |
| Cloud Run | ~$0–2 USD（極低流量） |
| Firebase Auth + Hosting | 免費 |
| Cloud Build | 免費 |
| Artifact Registry | ~$0.10 USD |
| Cloud Scheduler (6 jobs) | ~$0.30 USD |
| Cloud Logging / Monitoring | 免費 tier |
| Resend | 低量通知可控 |
| **合計** | **~$9–13 USD（約 270–400 TWD）** |

> 遠低於 1500 TWD 上限，有充裕空間。

---

### 可觀測性

- **Logging**：Cloud Logging
  - Go 使用 `log/slog`（標準庫，Go 1.21+）輸出 JSON structured log
  - Gin middleware 記錄每個 request：method、path、status、latency、user_id、property_id
  - 錯誤 log 包含 error code、stack trace
- **Metrics**：Cloud Monitoring
  - Cloud Run 自動提供：request count、latency、instance count
  - Cloud SQL 自動提供：CPU、memory、connection count
  - 設定基本 alerting policy：Cloud Run error rate > 5% 發 email 通知
- **Tracing**：不實作（單體 + 10 人工具，無此需求）
- **理由**：GCP 原生方案，零額外成本，無需 Prometheus/Grafana

---

### Error Handling

- **策略**：Gin middleware 統一處理，domain 層 error 往上傳遞，API 層轉換為 HTTP response
- **格式**：對應 OpenAPI spec 的統一 error response
  ```json
  {
    "code": "LEASE_HAS_UNPAID_BILLS",
    "message": "...",
    "details": { ... }
  }
  ```
- **Domain error**：定義 `DomainError` struct，含 `Code`（對應 OpenAPI error code）、`HTTPStatus`、`Details`
- **Panic recovery**：Gin 內建 Recovery middleware，捕捉 panic 回傳 500，並寫入 Cloud Logging

---

## [需確認]

**1. Cloud SQL 實例大小**
db-f1-micro 是共享 CPU，偶爾會有 CPU throttling。對 10 人工具通常沒問題，但若排程任務（月結快照、逾期掃描）與一般操作同時執行時可能出現短暫延遲。
- 選項 A：維持 db-f1-micro（約 $8/月，接受偶發延遲）
- 選項 B：升級為 db-g1-small（約 $25/月，穩定 CPU，仍在預算內）

**2. Cloud Run 冷啟動**
最小實例設為 0 時，若長時間無請求後第一個 request 會有 1–3 秒冷啟動延遲。對內部工具通常可接受。
- 選項 A：最小實例 = 0（省錢，約省 $5–8/月）
- 選項 B：最小實例 = 1（無冷啟動，增加約 $5–8/月費用，仍在預算內）
