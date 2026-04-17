# STDS Backend Vertical Slice Handbook

這份文件用這次實作的兩支 API 當範本：

- Read API：`GET /users/me`
- Write API：`POST /users`

目標不是教你一次把整個 backend 做完，而是讓你知道在目前 repo 狀態下，應該怎麼用最小套路，穩定地把下一支 API 從 OpenAPI 一路接到 DB。

---

## 為什麼先選這兩支 API

這兩支是目前最適合當第一批 vertical slice 的範例，原因很簡單：

- 都落在 `users` 這張已經存在的表，不需要再補 schema。
- 認證、授權、error handling、logging 都已經完成，可以直接重用。
- `GET /users/me` 可以示範最輕量的 read path。
- `POST /users` 可以示範最小可用的 write path：validation、application service、repository、conflict error mapping。
- 不會被更複雜的 domain rule、event、transaction 編排綁住。

這個選法符合 `docs/spec/tasks.md` 的精神：先建立一條完整的 query slice，再建立一條完整的 command slice。

---

## 這次新增了什麼

### 1. Read API：`GET /users/me`

路徑：

- middleware 已經先完成 Firebase 驗證，並把 principal 放進 Gin context。
- handler 從 request context 取出 `principal.user_id`
- handler 呼叫 `userRepo.FindByID`
- repository 查 `users` table
- handler 將 DB model mapping 成 OpenAPI response

對應程式：

- [internal/http/handler/api_server.go](/Users/cheni-fan/stds_backend/internal/http/handler/api_server.go:226)
- [internal/platform/database/users/repository.go](/Users/cheni-fan/stds_backend/internal/platform/database/users/repository.go:89)

### 2. Write API：`POST /users`

路徑：

- handler `ShouldBindJSON`
- handler 把 request 轉成 application input
- application service 做 validation
- application service 呼叫 repository `Create`
- repository 寫入 `users` table
- repository 將 unique constraint 衝突轉成語意化錯誤
- handler 回傳 `201`

對應程式：

- [internal/application/iam/create_user.go](/Users/cheni-fan/stds_backend/internal/application/iam/create_user.go:11)
- [internal/http/handler/api_server.go](/Users/cheni-fan/stds_backend/internal/http/handler/api_server.go:205)
- [internal/platform/database/users/repository.go](/Users/cheni-fan/stds_backend/internal/platform/database/users/repository.go:111)

### 3. Wiring

這次把原本空的 handler 實例改成可注入依賴：

- server 建立 repository 與 application service
- router 將依賴注入 `APIServer`

對應程式：

- [internal/server/server.go](/Users/cheni-fan/stds_backend/internal/server/server.go:42)
- [internal/http/router/router.go](/Users/cheni-fan/stds_backend/internal/http/router/router.go:17)

### 4. 測試

這次補的測試故意維持最小骨架，因為它們是你之後複製下一支 API 的模板。

- application service test
- router integration style test

對應程式：

- [internal/application/iam/create_user_test.go](/Users/cheni-fan/stds_backend/internal/application/iam/create_user_test.go:1)
- [internal/http/router/router_test.go](/Users/cheni-fan/stds_backend/internal/http/router/router_test.go:1)

---

## 目前已經可以直接重用的 Infra

你現在不用再重做以下東西，後續 API 直接沿用：

- OpenAPI codegen：`docs/spec/src` -> `internal/http/api/openapi.gen.go`
- Gin router 與 route registration
- Firebase token authentication middleware
- role authorization middleware
- property access authorization middleware
- request ID middleware
- recovery middleware
- structured logging middleware
- unified error handling middleware
- PostgreSQL connection bootstrap
- migration runner

關鍵檔案：

- [internal/http/router/router.go](/Users/cheni-fan/stds_backend/internal/http/router/router.go:16)
- [internal/http/middleware/auth.go](/Users/cheni-fan/stds_backend/internal/http/middleware/auth.go:14)
- [internal/http/middleware/authorization.go](/Users/cheni-fan/stds_backend/internal/http/middleware/authorization.go:16)
- [internal/http/middleware/error_handler.go](/Users/cheni-fan/stds_backend/internal/http/middleware/error_handler.go:14)
- [internal/platform/database/postgres.go](/Users/cheni-fan/stds_backend/internal/platform/database/postgres.go:14)

實務上你要記住的是：**新的 API 通常不需要再碰 middleware 與 server bootstrap，除非你真的引入了新的 cross-cutting concern。**

---

## Read API 該怎麼做

以 `GET /users/me` 為模板，下一支 read API 建議照這個順序：

1. 先確認 OpenAPI response shape 與 query parameter。
2. 決定資料來源是用 request principal、path param、query param。
3. 在 repository 補一個只服務這支 query 的方法。
4. 在 handler 中呼叫 repository。
5. 寫 response mapper，把 DB model 轉成 OpenAPI model。
6. 補最小 happy path 測試。

### Read API 最常見的坑

- 不要先做 generic query framework。
- 不要先抽象成 service layer，除非 read logic 已經明顯有複用。
- 不要直接把 DB row 結構裸回傳成 API response。
- 如果 OpenAPI 是 UUID/date-time 型別，mapping 時要注意轉型，不要把 generated type 當純 string。

### 什麼情況 read API 可以不走 application service

符合下面條件時，可以像 `GET /users/me` 一樣直接 `handler -> query repo`：

- 純查詢
- 沒有 transaction
- 沒有跨 aggregate 編排
- 沒有複雜 business rule

這不是偷懶，這是 `tasks.md` 已經明講的策略。

---

## Write API 該怎麼做

以 `POST /users` 為模板，下一支 write API 建議照這個順序：

1. handler 只做 request parsing 與 transport mapping。
2. application service 做 input validation 與 use case orchestration。
3. repository 只實作這支 use case 現在真正需要的方法。
4. repository 把 DB-specific error 轉成可辨識的 repo error。
5. application service 把 repo error 轉成 app error。
6. handler 只負責回傳 status code 與 response body。

### 為什麼 validation 放在 application service

因為這類 validation 屬於 use case input validation，不應該散落在 handler。

例如這次：

- `email` 必填且格式正確
- `name` 必填
- `role` 必填且必須是 `admin/organizer/staff/owner`

如果之後換成 CLI、job、internal call 也要重用同一套規則，放在 application service 才不會被 HTTP transport 綁死。

### Write API 最常見的坑

- 一開始就做全表 CRUD repository。
- 把 domain rule、validation、DB error handling 全塞在 handler。
- repository 直接回傳 HTTP error。
- 先做過度抽象的 transaction manager / unit of work。

---

## 這次的分層長什麼樣

這次不是 full DDD，而是務實版分層：

### Handler

責任：

- 收 request
- 讀 request context
- bind JSON
- 呼叫 application service 或 query repo
- 回 response

不該做：

- 寫 SQL
- 處理 unique constraint 細節
- 放 business rule

### Application Service

責任：

- use case input validation
- command orchestration
- repo error -> app error mapping

目前 `POST /users` 已經包含 Firebase user 建立、password reset link 產生與 email 通知協調，所以上面這層比最初版本稍厚，這是正常的。

### Repository

責任：

- 實際查 DB / 寫 DB
- 處理 row scan
- 處理 DB constraint error mapping

目前 `users` repository 已經示範了：

- `FindByFirebaseUID`
- `FindByID`
- `Create`

你之後新增 API 時，不要急著一次把 `Update/Delete/List/Count/...` 全補齊，只補當前 use case 要用的 method。

---

## 錯誤處理怎麼走

目前這個 repo 的錯誤流向是：

1. repository 回傳 repo-level error
2. application service 轉成 `apperr`
3. handler 用 `c.Error(err)`
4. middleware 統一轉成 OpenAPI `ErrorResponse`

這個方向是正確的，後面請持續沿用。

### 你可以直接沿用的錯誤型別

在 [internal/shared/apperr/common.go](/Users/cheni-fan/stds_backend/internal/shared/apperr/common.go:5) 已經有基礎錯誤，這次又補了：

- `USER_NOT_FOUND`
- `EMAIL_ALREADY_EXISTS`
- `FIREBASE_UID_ALREADY_EXISTS`
- `VALIDATION_EMAIL_REQUIRED`
- `VALIDATION_EMAIL_INVALID`
- `VALIDATION_NAME_REQUIRED`
- `VALIDATION_ROLE_REQUIRED`
- `VALIDATION_ROLE_INVALID`

原則：

- DB 細節不要直接穿透到 handler
- HTTP status 不要在 repository 決定
- middleware 是最後統一輸出的地方

---

## 下一支 API 你應該怎麼複製

### 如果是下一支 Read API

推薦順序：

1. `GET /properties/{id}`
2. `GET /properties`
3. `GET /bills`

做法：

- 先在對應 DB package 補 query method
- handler 直接呼叫 query repo
- 補 mapper
- 補 router test

### 如果是下一支 Write API

推薦順序：

1. `POST /properties`
2. `PATCH /users/{id}`
3. `POST /bills/{id}/meter`

做法：

- 先定 application input
- 先寫最小 validation
- repository 只補本次要用的方法
- 如果開始有 transaction，再把 transaction 放進 application service

---

## 什麼時候才需要 Aggregate / Event / Transaction

不是每支 API 一開始都需要。

### 可以先不用 Aggregate 的情況

- 純 read API
- 很單純的 create API，暫時沒有複雜 business rule

### 應該開始引入 Aggregate 的情況

- 狀態轉換開始有明確規則
- 同一支 command 會改多個欄位，且有不可違反的不變條件
- 需要 optimistic lock
- 需要發 domain event

例如之後這些就更適合進 aggregate：

- `POST /leases/{id}/terminate`
- `POST /leases/{id}/force-terminate`
- `POST /bills/{id}/payment`
- `POST /repair-requests/{id}/complete`

---

## 你之後開發時最值得注意的事

- 先選一支 API，不要先鋪一整個 BC。
- Read 與 Write 分開思考，不要硬套同一個流程。
- 每新增一層，都要能說出那層的責任。
- 每個 repository method 都要能對應一個明確 use case。
- 如果某個抽象目前只會被用一次，先不要抽。
- OpenAPI generated type 不是永遠等於 Go `string`，尤其是 UUID、Email、Date。
- middleware 已經很完整，下一步的價值主要在業務層，不在 infra。

---

## 本次實作後的最小複製模板

### Read API 模板

1. OpenAPI 已有 path 與 schema
2. `repository.FindXxx`
3. `handler.GetXxx`
4. `toXxxResponse`
5. router test

### Write API 模板

1. OpenAPI 已有 request/response schema
2. `application/<bc>/<use_case>.go`
3. `repository.Create/UpdateXxx`
4. `handler.Post/PatchXxx`
5. application test
6. router test

---

## 驗證方式

目前已驗證：

```bash
go test ./...
```

如果你要在本機手動驗證：

1. 啟動 PostgreSQL
2. 跑 migration
3. 啟 API
4. 用 Firebase emulator token 打 `GET /api/v1/users/me`
5. 用 admin/organizer 權限 token 打 `POST /api/v1/users`

---

## 一句話總結

你現在的 repo 已經不是「還要先補 infra 才能開發 API」，而是「可以直接用 vertical slice 持續長 API」的狀態。這次的 `GET /users/me` 與 `POST /users` 就是第一組可複製模板。
