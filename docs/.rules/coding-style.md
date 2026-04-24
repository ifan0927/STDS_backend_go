# Coding Style

## Scope

This document defines project-specific coding rules for STDS Backend.

General working rules such as assumption handling, simplicity, surgical changes, and goal-driven execution are defined in `AGENTS.md` and are not repeated here. This document only adds codebase-specific implementation rules.

## Layer Rules

- `internal/http` handles transport concerns only.
- `internal/application` owns use case orchestration, input validation, transactions, and cross-system coordination.
- `internal/platform/database` owns SQL, persistence, and row-to-struct mapping only.
- `internal/shared` is reserved for stable cross-cutting contracts that are reused across multiple layers.
- Domain events must be published only after transaction commit.

## Error Handling Rules

- Check `internal/shared/apperr/common.go` before creating a new reusable application error.
- If no shared error fits, define the error in the owning package. Do not centralize package-specific application errors only because they use `apperr.New`.
- Domain-layer sentinel errors must stay transport-neutral, usually with `errors.New`; map them to `apperr` only at the application or HTTP boundary.
- API-facing error message strings passed to `apperr.New` are written in English. The Traditional Chinese rule applies to developer conversation, not API response messages.
- Keep `WithCause` for internal debugging context, `WithDetails` for safe request-relevant details, and reserve `INTERNAL_SERVER_ERROR` as the final fallback for unmapped failures.
- Use `errors.Is` for repository, application, and domain sentinel checks so wrapped errors keep their intended mapping.
- Convert `sql.ErrNoRows` into a package-level not found error before mapping it at higher layers.
- **OpenAPI wrapper binding and parameter parsing errors must use the same API error schema as shared Gin error middleware; do not leave generated handlers on a fallback response shape such as `{"msg": ...}`.**

## HTTP And Gin Rules

- Handlers should parse requests, call services, and map responses only.
- Do not place business rules, authorization logic, or complex validation in handlers.
- Use `c.Error(...)` and let shared middleware produce the final error response.
- Keep request-scoped concerns in middleware.
- Property-scoped and resource-scoped authenticated routes must run the property resolver before authorization decisions. `admin` may skip only the access comparison, not UUID validation or resource-specific not-found mapping.
- Routes with optional `property_id` filters must use the shared query property resolver so unauthorized filters return the expected authorization error instead of falling through to empty repository results.
- Reuse shared query normalization helpers instead of reimplementing validation in handlers.
- **Recovery and any custom transport-level error path must reuse the same shared HTTP error writer; do not hand-write alternate JSON error bodies.**

## Application Service Rules

- Service constructors should accept only the dependencies required by the use case, and `Execute(...)` methods should validate and normalize input before running the main flow.
- Use `internal/platform/database/txrunner.Runner` for transactional use cases instead of open-coded transaction handling.
- Application services must not depend on Gin types or HTTP-specific behavior.
- **Application services must not import concrete database adapter packages such as `internal/platform/database/users` or `internal/platform/database/properties` for business-facing dependencies. Define and depend on ports in `internal/application/...` or `internal/domain/...`, and wire platform implementations at the composition root.**
- **When a use case needs persistence, the consuming application package must own the port interface, request/response structs, and sentinel semantics it depends on. Infrastructure packages may implement that port, but they must not define the contract that application code imports.**

## Repository And Database Rules

- Repositories should contain SQL, scan helpers, and persistence-specific error translation only.
- Repositories must not return Gin-specific or HTTP-specific concepts.
- Add operation context when wrapping unexpected database failures.
- Use package-level sentinel errors only for stable repository semantics such as not found or uniqueness conflicts.
- Transactional write methods should explicitly accept `*sql.Tx`.
- Preserve nullable database semantics at application/domain boundaries. Do not silently coerce `NULL` into zero values that have business meaning.
- Decode schemaless JSON columns defensively. If legacy/schema data permits arbitrary JSON shapes, scan into `any` or `json.RawMessage` before narrowing to an object type.
- **Repositories must not return `apperr.Error` values directly; repository code should return repository/domain semantics plus wrapped persistence context, and application or HTTP layers should map those errors to API-facing contracts.**
- **If a repository implementation needs to satisfy an application/domain port, keep the port owned by the consuming layer and have the `internal/platform/database/...` package implement it; do not make the application layer import the database package just to obtain an interface or DTO type.**

## Shared Utilities Rules

- Reuse existing shared helpers and wiring patterns such as `internal/http/queryparams`, `internal/http/requestctx`, `internal/shared/apperr`, and `internal/platform/database/txrunner` before adding new middleware, helpers, or orchestration paths.
- **Composition-root adapters in `internal/server` should be grouped by consuming application slice or responsibility, such as `iam_adapters.go`, `property_adapters.go`, and `jobs_adapters.go`; do not accumulate unrelated adapters in a misleading file name.**

## Testing Rules

- Prefer Go standard library `testing`, use `t.Run` for scenario groups, and keep assertions explicit and behavior-focused.
- When behavior changes, add or update the narrowest focused test that proves the expected outcome.
- Handler/router fakes should capture important input IDs, filters, and params when the test's purpose includes request binding, authorization forwarding, or service wiring.
- Domain aggregate `State()` snapshots must not expose mutable internal state. When state contains pointers, slices, maps, or JSON-like values, add tests that mutate the returned snapshot and verify the aggregate is unchanged.
- Run the smallest relevant test set after making the change.

## Go Style Rules

- Follow idiomatic Go and the surrounding project style.
- Prefer clear names, minimal comments, and interfaces only when a consumer needs an abstraction.
- Do not add wrapper functions, configurability, or extension points that were not requested.

## Anti-Patterns For Agents

- Starting implementation before unresolved assumptions are made explicit.
- Picking one interpretation of an ambiguous requirement without saying so.
- Adding abstractions, helpers, or configuration for future scenarios that are not part of the task.
- Refactoring, renaming, reformatting, or reorganizing unrelated code while making a focused change.
- Mapping every failure to `INTERNAL_SERVER_ERROR` without checking for a more specific error.
- Writing business rules, authorization checks, or repeated validation directly in handlers.
- Reimplementing helpers, middleware behavior, pagination logic, or shared error mapping that already exists in the codebase.
- Returning transport-level semantics directly from repository code.
- Changing behavior without adding the smallest relevant test or without running the relevant tests.
