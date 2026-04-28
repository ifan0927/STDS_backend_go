# Infra Guideline

產出日期：2026-04-16  
適用範圍：STDS Backend（Go + Gin + PostgreSQL + Firebase Auth + Cloud Run）

---

## 文件目的

這份文件的目的不是盤點 repo 目前有哪些 infra 元件，而是回答兩個開發時真正會遇到的問題：

1. 這個專案的 infra 設計策略是什麼？
2. 我在日常開發 API / scheduler / repository 時，應該怎麼正確使用它？

這份文件聚焦在：

- logging
- error handling
- auth / authorization
- config
- database / repository / transaction
- scheduler trigger

---

## 一句話策略

這個專案的 infra 策略是：

- **把 cross-cutting concern 固定在少數幾個入口**
- **讓 handler / service / repository 可以各司其職**
- **避免每個 vertical slice 重做 logging、error response、auth、DB bootstrap**

換句話說，後續開發應該是「沿用固定骨架」，不是「每做一支 API 就再發明一次基礎設施」。

---

## 設計原則

### 1. Cross-cutting concern 集中，不分散

以下 concern 不應散落在各 handler：

- request id
- panic recovery
- access logging
- standardized error response
- Firebase authentication
- role authorization
- property authorization

這些都應由 middleware 或 server bootstrap 集中處理。

### 2. 讓錯誤往上流，讓 response 在最外層統一轉

錯誤處理策略不是「哪裡出錯就哪裡直接回 HTTP」，而是：

1. 下層回傳 error
2. 上層視需要轉成更有語意的 error
3. 最後由 HTTP middleware 統一轉成 API response

### 3. 成功請求靠 access log，失敗請求靠 request id + error log

這個專案的 logging 不追求每層都打滿，而是只保留真正能協助定位問題的資訊：

- 每個 request 一筆 access log
- 每個 panic 一筆 recovery error log
- 每個非 panic 的 5xx 一筆 structured error log
- 少量高價值的業務 / 批次 / 外部整合 log

### 4. Infrastructure 不應主導業務設計

- 不先做 generic framework
- 不先做全表 CRUD repository
- 不先做複雜 observability abstraction
- 不先做過度彈性的 middleware config

只補目前專案規模真的會用到的能力。

---

## 你開發時要先知道的骨架

### 啟動流程

實際入口：

1. `cmd/api/main.go`
2. `internal/config/config.go`
3. `internal/server/server.go`

流程：

1. `config.Load()` 載入環境變數
2. `logging.NewBootstrap()` 處理 config 前期失敗 log
3. `logging.New(cfg.App)` 建立正式 app logger
4. `server.New(cfg)` 建立 DB、Firebase、repositories、services、router
5. `app.Run()` 啟動 Gin / HTTP server

### HTTP request 流程

主要入口：

- `internal/http/router/router.go`
- `internal/http/middleware/*`

固定順序：

1. `RequestID`
2. `Recovery`
3. `Logging`
4. `ErrorHandler`
5. protected route 再進 `Auth` 或 `RequireSchedulerKey`
6. `RequireRoles`
7. `RequirePropertyAccess`
8. handler

你在新 API 開發時，**原則上不應改這條 middleware 鏈**，除非你真的新增新的 cross-cutting concern。

補充：

- 這條鏈可以維持不變，但 `RequirePropertyAccess` 使用的 resolver 必須攜帶正確的 resource not-found 語意
- 不要把 resource lookup 的 `not found` 一律壓成 `FORBIDDEN`
- 對 `/properties/:id/...` 這種直接吃 property id 的 route，也要維持 `PROPERTY_NOT_FOUND` 與 `FORBIDDEN` 的區分

---

## Logging Strategy

### 現在的 logging 設計

logger 入口：

- `internal/platform/logging/logger.go`

目前固定做法：

- 使用標準庫 `log/slog`
- JSON structured logging
- 預設欄位含 `service`、`env`
- local 預設 `debug`
- 非 local 預設 `info`

這個選型對 Cloud Logging 是正確且足夠的，不需要再導入 zap / logrus / OpenTelemetry logging wrapper。

### 你開發時怎麼判斷要不要記 log

#### 不需要額外記 log 的情況

- 一般 API happy path
- 單純 validation error
- handler 成功回應
- repository 查詢失敗但會正常往上轉成既有 error

原因：

- access log 已經會記 request 結果
- 如果每一層都再打一次，Cloud Logging 只會變噪音

#### 應該額外記 log 的情況

- 外部系統呼叫失敗
- scheduler job 開始 / 完成 / 失敗
- retry / fallback 發生
- 跨資源編排流程中某個關鍵狀態轉換
- 啟動 / bootstrap 失敗

### 目前固定的 access log 欄位

HTTP middleware 會自動記錄：

- `request_id`
- `method`
- `path`
- `status`
- `latency_ms`
- `user_id`
- `firebase_uid`
- `role`
- `property_id`
- `error_code`
- scheduler 相關欄位

使用上要知道兩件事：

1. `property_id` 現在記錄的是「真正解析後的 property id」，不是單純 route param
2. `error_code` 由 error handling middleware 決定，不是 handler 自己手填

### Logging 分層規則

#### Handler

- 通常不主動記成功 log
- 發生錯誤時用 `c.Error(err)`，不要先 log 再回 error，除非你真的需要額外業務上下文

#### Application Service

- 只在跨資源流程、外部整合、批次流程需要額外觀測時記 log
- 避免把每個錯誤都先 log 一次再往上丟

#### Repository

- 不主動打 SQL error log
- 將 driver / SQL error 包裝後回傳上層
- 不直接產生 HTTP response

### Sensitive Data Logging 規則

這條規則現在請視為固定規範：

- **不要直接 `slog.Any(request)`**
- **不要直接 log 原始 request body**
- **不要 log Authorization header、token、password、credentials file content**
- **request 衍生欄位一律白名單式 logging**

專案內提供的 helper：

- `internal/platform/logging.AllowlistAttrs`

使用方式：

```go
logger.Info("create user request",
	logging.AllowlistAttrs(map[string]any{
		"email": request.Email,
		"role": request.Role,
		"name": request.Name,
		"password": request.Password,
	}, "email", "role", "name")...,
)
```

這種做法的目的不是抽象化 logging，而是避免有人順手把整包 request 打出去。

### 非 panic 5xx 的 logging 策略

`ErrorHandler` 現在會在 `HTTPStatus >= 500` 時補一筆 structured error log，至少含：

- `request_id`
- `method`
- `path`
- `error_code`
- `cause`

這代表：

- client 看到的是安全的 `500` response
- GCP Cloud Logging 查得到真正的伺服器端錯誤上下文

---

## Error Handling Strategy

### 核心策略

這個專案的 error handling 流向是：

1. repository / adapter 回傳 error
2. application service 視需要轉成語意化 error
3. handler 呼叫 `c.Error(err)` 往上交
4. `ErrorHandler` 統一輸出 `ErrorResponse`
5. `Recovery` 專責 panic

### 共用 error contract

位置：

- `internal/shared/apperr/error.go`
- `internal/shared/apperr/common.go`

共用型別：

- `Code`
- `Message`
- `HTTPStatus`
- `Details`
- `Cause`

用途：

- `Code`：對外穩定 contract
- `Message`：client 可見訊息
- `HTTPStatus`：transport mapping
- `Details`：必要摘要
- `Cause`：伺服器端追查用，不直接暴露給 client

### 你開發時 error 應該怎麼拋

#### Handler

Handler 原則：

- 負責 parse request
- 呼叫 service / repo
- 若有錯誤，使用 `c.Error(err)`，不要自行組錯誤 JSON

範例：

```go
result, err := s.createUserService.Execute(c.Request.Context(), input)
if err != nil {
	c.Error(err)
	return
}
```

#### Application Service

Application service 原則：

- 做 use case input validation
- 做 repo error -> app error mapping
- 必要時加 business context

範例方向：

```go
if input.Email == "" {
	return nil, apperr.ErrValidationEmailRequired
}

user, err := s.userRepo.Create(ctx, params)
if err != nil {
	if errors.Is(err, users.ErrEmailConflict) {
		return nil, apperr.ErrEmailAlreadyExists.WithCause(err)
	}
	return nil, apperr.ErrInternalServerError.WithCause(err)
}
```

#### Domain

Domain 層原則：

- 盡量不要直接知道 HTTP status
- 可以有自己的 business error
- 若是 domain error，通常在 application service 轉成 `apperr`

#### Repository

Repository 原則：

- 把 DB-specific 細節收斂起來
- 將 unique constraint / not found / conflict 轉成 repo-level 可辨識錯誤
- 不直接回 HTTP response

### 你開發時 error 怎麼轉

建議順序：

1. DB / Firebase / 外部 client 先回原始錯誤
2. repository / adapter 轉成較穩定的 infra or repo error
3. application service 轉成 `apperr`
4. handler 用 `c.Error(err)`
5. middleware 統一輸出

### 什麼時候該直接用 `apperr`

適合直接用 `apperr` 的情況：

- request validation error
- authentication / authorization error
- cross-cutting shared error
- use case 層已經很明確的 business response

不適合直接把所有錯誤都寫成 shared `apperr` 的情況：

- BC 專屬 business rule error
- 尚未穩定的 domain vocabulary

這類應先放在該 BC 附近，再由 application service 決定怎麼映射。

### 5xx 與 panic 的差別

#### 非 panic 5xx

- 仍走 `ErrorHandler`
- client 看到標準化 `500`
- details 含 `request_id`
- server 端會多一筆 structured error log

#### panic

- 由 `Recovery` 接住
- client 看到 `500`
- server 端會記 `panic recovered` 與 stack trace

### 常見錯誤寫法，這個專案不要用

- handler 直接 `c.JSON(...error...)`
- repository 直接回 `apperr` + HTTP 語意
- 每層都 log 同一個 error
- 把 `err.Error()` 原文直接回給 client

---

## Auth / Authorization Strategy

### Auth 設計

入口：

- `internal/platform/firebase/auth.go`
- `internal/http/middleware/auth.go`

策略：

- API authentication 使用 Firebase ID token
- Firebase custom claims 承載 `role` 與 `assigned_property_ids`
- middleware 驗證 token 後，仍會載入 DB user
- request context 內最終使用的是 normalized principal

你開發時要記得：

- handler 不應自己 parse `Authorization` header
- handler 需要 user 身份時，從 `requestctx.GetPrincipal(c)` 取

### Authorization 設計

入口：

- `internal/http/middleware/authorization.go`
- `internal/http/router/router.go`

策略拆成兩段：

1. `RequireRoles`
2. `RequirePropertyAccess`

也就是：

- 先判斷角色
- 再判斷 property 範圍

你新增 protected route 時，應該透過 router policy 指定：

- 這支 route 用 Firebase auth 還是 scheduler key auth
- 允許哪些 role
- 是否需要 property resolver

這裡的重點是：

- **auth / authorization 骨架本身已經先建好了**
- checklist 提到的 `auth`，不是要你重寫 middleware
- 而是要你在 `internal/http/router/router.go` 的 route policy 裡，明確接上這支 API 要用哪種既有保護規則

另外要注意：

- 如果 route 是 `ResourcePropertyID(...)` 這類先 lookup resource 再回推 property scope 的模式，router policy 不能只表達「怎麼找到 property」
- router policy 還必須同時表達「這個 resource 不存在時要回哪個 not-found error code」
- 例如 room route 應回 `ROOM_NOT_FOUND`，bill route 應回 `BILL_NOT_FOUND`，不能共用一個模糊的 `FORBIDDEN`

### Scheduler Auth

入口：

- `internal/http/middleware/scheduler.go`

策略：

- 不是走 Firebase
- 改用 `X-Scheduler-Key`
- key 由 config 提供

這類 endpoint 的使用原則是：

- 只給 Cloud Scheduler 或內部受控 caller
- 不要混用一般使用者 auth

---

## Config Strategy

### Config 載入規則

入口：

- `internal/config/config.go`

原則：

- 所有 runtime config 都只從 `config.Load()` 進來
- handler / repository / service 不直接讀 `os.Getenv`
- timeout、scheduler key、DB URL、Firebase 設定都交給 config

### 你開發時怎麼擴充 config

正確做法：

1. 在 `Config` struct 新增欄位
2. 在 `Load()` 補載入邏輯
3. 視需要補 validation
4. 由 `server.New()` 注入下游依賴

錯誤做法：

- 在 repository 裡直接讀 env
- 在 middleware 裡直接讀 env

---

## Database / Repository / Transaction Strategy

### DB 連線策略

入口：

- `internal/platform/database/postgres.go`

目前做法：

- 使用 `database/sql`
- driver 為 `pgx`
- 啟動時 `PingContext` 驗證連線

你開發時要知道：

- DB pool 在 `server.New()` 開一次
- repositories 共用同一個 `*sql.DB`
- 不要在 handler / service 自己 `sql.Open`

### Repository 策略

原則：

- repository 為 use case 服務，不為 table 完整 CRUD 服務
- read path 與 write path 可以分開
- query repository 服務 read model / 報表 / scheduler scan
- aggregate repository 服務 command / state change

### Transaction 策略

原則：

- transaction 邊界放在 application service
- repository 不自行偷偷開 transaction
- 只有真的要保證原子性時才包 transaction

如果流程是：

- 建立資料
- 更新另一張表
- 發送 domain event

這種流程應放在 application service 編排，不要拆在多個 handler / repo 各自做。

### DB 錯誤怎麼處理

正確做法：

- repository 解析 not found / conflict / concurrency 類型
- application service 決定要不要轉成 `apperr`
- response 統一由 middleware 輸出

不要做的事：

- 直接把 driver error 原文回給 client
- 在 handler 解析 SQL constraint name

---

## 80% 開發 Checklist

這一節可以當成你平常開發時的主 checklist。  
它不是 100% 覆蓋所有情況，但對這個專案來說，**大約可以覆蓋 80% 的 API 開發工作**。

你可以先照這份 checklist 走；只有在遇到明顯 business rule、狀態轉換、transaction、跨 aggregate 協調時，才升級成更完整的 domain / application flow。

### 先判斷你現在做的是哪一類

#### 類型 A：Read API

符合以下條件，多半就是 Read API：

- 純查詢
- 不改變狀態
- 不需要 transaction
- 沒有明顯 business rule
- 回傳的是 list / detail / dashboard / report / query result

這類通常走：

`handler -> query repository -> response`

#### 類型 B：簡單 Write API

符合以下條件，多半是簡單 Write API：

- 有新增或修改資料
- 規則還很薄
- 不需要 aggregate 保護複雜 invariant
- transaction 範圍很小或沒有
- 錯誤主要是 validation / conflict / not found

這類通常走：

`handler -> application service -> repository -> response`

#### 類型 C：完整 Command / Aggregate API

符合以下任一條件，就不要只靠簡單 checklist，應考慮 aggregate / domain model：

- 有明確 business rule
- 有狀態轉換
- 一次操作會影響多個欄位且必須一起成立
- 需要保護 invariant
- 需要 transaction
- 需要 event
- 需要跨 aggregate / bounded context 協調

這類通常走：

`handler -> application service -> aggregate/domain -> repository -> event`

### Read API Checklist

這份 checklist 適用於大多數查詢 API。

1. 先確認 OpenAPI contract
   - response shape
   - query params / path params
   - role / property access 要求
2. 在 router policy 補 route 規則
   - auth strategy
   - allowed roles
   - property resolver
3. 決定資料來源
   - path param
   - query param
   - request principal
4. 建立或補 query repository method
   - 只做這支 API 需要的查詢
   - 不先抽 generic query framework
5. 在 handler 中 parse request 並呼叫 query repository
6. 將查詢結果 mapping 成 API response model
7. 若出錯，用 `c.Error(err)`
8. 補最小測試
   - happy path
   - 最重要的 auth / not found / filter path

Read API 的預設原則：

- **先不要硬做 application service**
- **先不要硬做 aggregate**
- **先不要把 read model 跟 domain model 綁在一起**

只有當 read logic 開始出現真正複用價值時，才考慮再抽 service。

### 簡單 Write API Checklist

這份 checklist 適用於多數「有寫入，但規則還不重」的 API。

1. 先確認 OpenAPI contract
   - request body
   - response body
   - error code
2. 在 router policy 補 route 規則
   - auth strategy
   - allowed roles
   - property resolver
3. 在 handler 做 transport parsing
   - `ShouldBindJSON`
   - path/query param parse
   - 轉成 application input
4. 在 application service 做 use case validation
   - required field
   - input format
   - 基本 business precondition
5. repository 只實作這支 use case 需要的方法
6. application service 做 error mapping
   - repo error -> `apperr`
7. handler 正常回 response；錯誤則 `c.Error(err)`
8. 補最小測試
   - happy path
   - validation path
   - conflict / not found path

簡單 Write API 的預設原則：

- **validation 放 application service，不要散在 handler**
- **repository 不直接回 HTTP error**
- **沒有明確複雜規則時，不要急著做 aggregate**

### 什麼時候要升級成 Aggregate / Domain Flow

當你寫 Write API 時，如果遇到下面情況，就表示「簡單 Write checklist 不夠了」：

- 操作不是單純 create/update，而是狀態轉換
- 有「只有在某條件成立時才能操作」這類 invariant
- 同一筆資料的多個欄位必須一起維持一致
- 同一個 use case 需要更新多個 repository
- transaction 成敗必須一致
- 完成後要送 domain event

這時建議流程改成：

1. handler 負責 transport parsing
2. application service 負責 orchestration / transaction
3. aggregate / domain method 負責 business rule
4. repository 負責 persistence
5. transaction commit 後再處理 event / side effect

### Aggregate / Domain 不熟時，先怎麼判斷

你可以先用這個簡單規則：

- 如果這支 API 只是「把輸入寫進 DB」：先用簡單 Write checklist
- 如果這支 API 是「改變某個 business 狀態」：停下來評估 aggregate
- 如果你已經開始寫出一堆 `if status == ...`、`if already ...`、`if unpaid ...`：多半該進 domain
- 如果你發現 handler 或 service 裡充滿規則判斷：多半代表 aggregate 該出現了

### 開發時的最小決策順序

每次新 API 可以先照這個順序問自己：

1. 這支是 read 還是 write？
2. 這支 write 是簡單寫入，還是 business command？
3. 需不需要 transaction？
4. 需不需要 aggregate？
5. auth / role / property access 要怎麼接？
6. error code 要在哪一層決定？
7. access log 已經夠不夠？需不需要額外業務 log？

只要你能先回答這七題，實作大多不會走偏。

### Checklist 裡的 auth 現在到底代表什麼

如果你看到 checklist 裡寫：

- auth strategy
- allowed roles
- property resolver

它的意思是「**把新 route 掛到既有 auth / authorization 骨架上**」，不是「重做一套 auth」。

以目前 repo 來說，已經存在的部分是：

- `middleware.Auth(...)`：驗 Firebase token、載入 principal
- `middleware.RequireRoles(...)`：驗角色
- `middleware.RequirePropertyAccess(...)`：驗 property scope
- `middleware.RequireSchedulerKey(...)`：保護 scheduler endpoint
- `routePolicies(...)`：把每條 route 要套哪個規則集中宣告

所以你在 checklist 實際要做的是：

1. 決定這條 route 是 Firebase 還是 scheduler auth
2. 決定允許哪些角色
3. 如果是 property-scoped route，決定 property id 從 path param 還是 resource ownership resolver 取得

換句話說，**policy 已存在；每支新 API 要做的是把 policy 接對，不是把 auth 重做一遍**。

---

## Scheduler Strategy

入口：

- `internal/application/jobs/trigger.go`
- `internal/http/handler/api_server.go`

設計重點：

- 外部 scheduler 打受保護 endpoint
- job trigger service 處理 dedupe / locking / timeout / retry metadata
- handler 回 normalized trigger result
- access log 與 request context 帶上 job metadata

你新增 job 時，應該把重點放在：

- business execution logic
- query / scan 實作
- idempotency
- summary output

不要重做：

- scheduler auth
- run tracking
- retry metadata shape

---

## 開發時的推薦使用方式

### 新增 Read API

做法：

1. handler parse request
2. 直接呼叫 query repository 或薄 application service
3. 錯誤用 `c.Error(err)`
4. 正常回 response

不要做：

- 為純查詢硬抽 transaction manager
- 為純查詢硬做 aggregate

### 新增 Write API

做法：

1. handler bind request
2. application service 做 validation 與 orchestration
3. repository 做 persistence
4. service 將 repo / domain error 轉成 `apperr`
5. handler 用 `c.Error(err)`

### 新增受保護 route

做法：

1. 在 router policy 補 method/path
2. 指定 auth strategy
3. 指定 allowed roles
4. 若需要 property scope，補 property resolver

### 新增 external adapter

做法：

1. 放在 `internal/platform`
2. 對外暴露小而穩定的 interface
3. 回傳明確 error
4. 由 application service 決定語意化 mapping

---

## 常見坑

### 1. Handler 寫太多

症狀：

- validation 全塞在 handler
- repo error mapping 全塞在 handler
- handler 自己回錯誤 JSON

修正：

- transport parsing 留在 handler
- validation / orchestration 移到 application service
- error response 交給 middleware

### 2. 同一個錯誤被 log 三次

症狀：

- repository log 一次
- service log 一次
- middleware 再 log 一次

修正：

- 預設只保留最有價值的一次
- request log + 5xx error log 已經足夠大多數問題

### 3. 把敏感欄位整包打出去

症狀：

- `slog.Any("request", req)`
- log 原始 body
- log token / password

修正：

- 一律使用 allowlist field logging

### 4. 在下層直接帶 HTTP 語意

症狀：

- repository 直接回 `http.StatusConflict`
- domain rule 直接認識 transport format

修正：

- HTTP 語意留在 `apperr` 與 transport 邊界

---

## 目前結論

現在這個 repo 的 infra 狀態是：

- logging 已達到可在 GCP Cloud Logging 穩定使用的程度
- error handling contract 已固定
- auth / authorization / scheduler 骨架已可直接重用
- database bootstrap 與 repository 邊界已足夠支撐後續 vertical slice

後續開發的重點不應再放在「要不要重做 infra」，而應放在：

- 用既有骨架穩定長出新的 API / use case
- 保持 error 與 logging 寫法一致
- 不讓新的 slice 把 cross-cutting concern 再次打散

---

## Aggregate 最小範例

如果你想看一個「什麼情況該升級成 aggregate」的最小案例，我會選：

`POST /repair-requests/:id/assign`

原因：

- 它不是單純把欄位寫進 DB
- 它有明確狀態轉換：`open -> assigned`
- 它有 invariant：已完成、已取消的 repair request 不能再 assign
- 它比 lease terminate、bill payment 更單純，適合當第一個 aggregate 範例

### 這個案例的業務規則

假設這支 API 的需求是：

- 指派維修單給某位處理人員
- 只有 `open` 狀態可指派
- 已經 `assigned` 的單不能重複 assign
- `completed` / `cancelled` 的單不能再 assign
- assign 成功後要寫入 `assignee_id`、`assigned_at`，並把狀態改成 `assigned`

這時如果你只用「handler 收 request -> repository 直接 update」，
business rule 很容易散在 service 或 handler 的 `if` 裡。

比較穩的切法會是：

`handler -> application service -> repair request aggregate -> repository`

### 這個範例會改哪些檔案

以下是建議的最小檔案切分。

1. `internal/http/handler/api_server.go`
   - 新增 `POST /repair-requests/:id/assign` handler
   - 只做 request parsing、取 principal、呼叫 application service、回 response / `c.Error(err)`

2. `internal/application/journal/assign_repair_request.go`
   - 建立 use case service
   - 負責 transaction、載入 aggregate、呼叫 aggregate method、存回 repository
   - 把 repo/domain error 轉成 `apperr`

3. `internal/domain/journal/repair_request.go`
   - 放 `RepairRequest` aggregate 與 `Assign(...)` method
   - business rule 寫在這裡，例如狀態檢查、不可重複指派

4. `internal/platform/database/repairrequests/repository.go`
   - 補 aggregate 需要的 persistence 方法，例如 `GetByIDForUpdate`、`Save`
   - 不直接承擔 business rule

5. `internal/http/router/router.go`
   - 補或確認 route policy
   - 這個案例其實 auth 骨架已經有了，你只要把 route 接到 `authStrategyFirebase + allowedRoles + propertyResolver`

6. 對應測試檔
   - aggregate test：驗狀態規則
   - application service test：驗 transaction / error mapping
   - handler test：驗 auth、binding、response

### 每層大概長什麼樣子

#### Handler

責任只有 transport：

- 讀 path param `id`
- bind request body
- 從 `requestctx.GetPrincipal(c)` 拿操作者
- 呼叫 `AssignRepairRequestService`

不應該做：

- `if status == completed`
- `if assignee already set`
- 直接寫 SQL update

#### Application Service

責任是 orchestration：

- 開 transaction
- 從 repository 載入 repair request aggregate
- 呼叫 `aggregate.Assign(...)`
- 存回 repository
- 視需要發 event / audit log

這層可以知道：

- 要不要 transaction
- repo not found 要轉成什麼 `apperr`
- command 執行成功後要不要觸發後續 side effect

#### Aggregate

責任是 business rule：

```go
func (r *RepairRequest) Assign(assigneeID string, now time.Time) error {
	if r.Status == RepairRequestStatusCompleted || r.Status == RepairRequestStatusCancelled {
		return ErrRepairRequestNotAssignable
	}
	if r.Status == RepairRequestStatusAssigned {
		return ErrRepairRequestAlreadyAssigned
	}

	r.AssigneeID = assigneeID
	r.AssignedAt = &now
	r.Status = RepairRequestStatusAssigned
	r.touch(now)

	return nil
}
```

這種規則放在 aggregate 的好處是：

- 同一條規則不會散在不同 handler / service
- 以後如果還有 scheduler、internal command 也要 assign，同一份規則可直接共用
- 測試會變得很集中

### 為什麼這個案例適合當第一個 aggregate 範例

因為它同時滿足三件事：

- 有清楚的 command 動作，不只是 CRUD
- 有明確 state transition
- 複雜度還不高，不需要一開始就帶 event bus 或跨 aggregate 協調

所以它剛好可以示範：

- 什麼時候 simple write 不夠
- aggregate 應該承擔哪些責任
- auth / handler / service / repository / aggregate 的邊界怎麼切

### 這個範例刻意沒有做的事

為了保持「最小範例」，先不要一起塞進去：

- domain event bus
- notification adapter
- generic command framework
- 過度抽象的 base aggregate

先把一條 command slice 做乾淨，比先做 framework 更重要。

---

## In-Process PubSub / Domain Events

### 先講結論

目前專案提供的是 **in-process event bus**，不是 external message broker。

也就是說，這裡的「pubsub」目前指的是：

- 同一個 Go process 內發 event
- 同步呼叫 subscriber
- 主要用來承接 transaction commit 後的 follow-up action

它**不是**：

- GCP Pub/Sub
- Kafka
- RabbitMQ
- 跨服務非同步訊息系統

如果未來真的要做跨服務事件傳遞，再另外設計 message broker adapter；不要把目前這個 in-process bus 誤認成外部基礎設施。

### 目前提供了哪些元件

#### 1. Domain contract

位置：

- `internal/domain/events/publisher.go`

這層只定義 contract：

```go
type Publisher interface {
	Publish(ctx context.Context, event any) error
}
```

這個設計是刻意的：

- domain / application 只依賴 `Publisher` 介面
- 不直接依賴 infra 的 concrete implementation
- infra 可以提供真正的 event bus，也可以提供 noop fallback

另外目前也有：

- `domainevents.NoopPublisher{}`

用途是：

- 在 event wiring 尚未啟用時，提供安全預設
- 避免 transaction runner 因為 nil publisher 爆掉

#### 2. Infra implementation

位置：

- `internal/platform/eventbus/bus.go`

目前 infra 提供的是 `eventbus.Bus`：

- 實作 `domainevents.Publisher`
- 以 event concrete type 做 subscriber match
- 同步、依註冊順序呼叫 handlers
- 任一 handler error，會中止並回傳 error

這代表它適合：

- 同 process 的 follow-up logic
- 輕量、可預期的 post-commit side effect

這代表它目前不適合：

- 長時間背景工作
- 需要 retry queue 的工作
- 跨服務整合

#### 3. Transaction publication point

位置：

- `internal/platform/database/txrunner/runner.go`

目前事件發布時機是：

1. use case 在 transaction 中呼叫 `recorder.Record(event)`
2. DB commit 成功
3. `txRunner` 逐筆呼叫 `publisher.Publish(ctx, event)`

這個順序很重要，因為它保證：

- transaction rollback 時不會發 event
- 只有 commit 成功後才會觸發 subscriber

### 目前正式 wiring 狀態

目前 `server.New()` 會建立 runtime event bus：

```go
bus := eventbus.New()
txRunner := dbtxrunner.New(db, bus)
```

位置：

- `internal/server/server.go`

正式 runtime 的行為是：

- application service 在 DB transaction 內透過 `EventRecorder` 記錄 event
- `txRunner` 只在 commit 成功後呼叫 `bus.Publish(ctx, event)`
- `eventbus.Bus` 依 concrete event type 同步呼叫已註冊 subscriber
- subscriber 由 composition root 明確註冊，不在 request flow 中動態註冊

目前已註冊的 runtime subscribers：

- `UserPasswordResetRequested -> notificationService.HandleUserPasswordResetRequested`
- `LeaseCreated -> OccupyRoomOnLeaseCreatedHandler.HandleLeaseCreated`
- `LeaseCreated -> ActivateTenantOnLeaseCreatedHandler.HandleLeaseCreated`
- `LeaseTerminated -> ReleaseRoomOnLeaseTerminatedHandler.HandleLeaseTerminated`
- `LeaseTerminated -> DeactivateTenantOnLeaseTerminatedHandler.HandleLeaseTerminated`
- `JournalExpenseRecorded -> JournalExpenseRecordedHandler.HandleJournalExpenseRecorded`

`domainevents.NoopPublisher{}` 仍是 `txrunner.New` 的 nil publisher fallback，並可用於不需要 dispatch event 的測試或局部 wiring；它不是目前 production `server.New()` 的 publisher。

### Boundary decision rule

目前 event bus 是同步、in-process、post-commit dispatch。它沒有 outbox、retry queue、dead-letter queue、durable delivery guarantee，也不是跨服務 broker。

因此 boundary 判斷規則是：

- 需要與 command transaction 強一致的 side effect，應留在 application service transaction 內直接編排。
- 可接受 command commit 後再補做、且失敗不需要 rollback 原 command 的 follow-up，才適合目前的 pub/sub subscriber。
- 若只是保留 domain trace、審計語意或未來擴充點，可以 publish event 但不註冊 subscriber；文件必須明確標成 reserved/published-only，不能暗示 runtime 已實作。

### 標準使用方式

#### 1. Domain event 定義在 `internal/domain/events`

例如：

```go
type PropertyCreated struct {
	PropertyID string
	OccurredAt time.Time
}
```

原則：

- event 是過去式，描述已經發生的事
- event payload 只放後續處理真的需要的欄位
- 不要把整個 aggregate 或 request body 丟進 event

#### 2. Application service 在 transaction 內 record event

例如：

```go
err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
	property, err := s.propertyRepo.Create(ctx, tx, params)
	if err != nil {
		return err
	}

	recorder.Record(domainevents.PropertyCreated{
		PropertyID: property.ID,
		OccurredAt: s.now().UTC(),
	})

	return nil
})
```

規則：

- 不要在 repository 裡直接 publish
- 不要在 transaction commit 前直接 publish
- 一律透過 `EventRecorder` 交給 `txRunner`

#### 3. Infra 用 `eventbus.Subscribe` 註冊 subscriber

例如：

```go
bus := eventbus.New()

eventbus.Subscribe(bus, func(ctx context.Context, event domainevents.PropertyCreated) error {
	// call follow-up service or adapter
	return nil
})
```

規則：

- subscriber 註冊屬於 bootstrap / wiring 責任
- 不要在 handler 或 use case 執行途中動態註冊
- subscriber 應該做明確、單一責任的 follow-up action

#### 4. `txRunner` 注入真正的 publisher

啟用時的 wiring 會長這樣：

```go
bus := eventbus.New()

eventbus.Subscribe(bus, func(ctx context.Context, event domainevents.PropertyCreated) error {
	return nil
})

txRunner := dbtxrunner.New(db, bus)
```

這樣 application 層仍然只知道 `domainevents.Publisher`，不需要知道 `eventbus.Bus` 的存在。

### 使用上的限制

這個 in-process bus 目前是同步模型，所以要明確接受以下限制：

- subscriber 變慢，原請求也會跟著變慢
- subscriber 回 error，整個 publish 會回 error
- 沒有內建 retry / dead-letter / backoff
- process crash 後不保證事件重送

所以這套機制適合：

- 更新本地 read model
- 寫 audit trail
- 觸發輕量內部流程

不適合：

- 寄信
- 長時間第三方 API 呼叫
- 關鍵非同步整合
- 需要 delivery guarantee 的工作

這些需求應該交給明確的 job / queue / external messaging 設計，不要硬塞在目前的 event bus 上。
