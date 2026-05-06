# Testing Rules

## Scope

This document defines test-writing rules for STDS Backend.

The goal is release confidence and PR gate quality, not a global coverage
percentage. Tests should prove observable behavior, layer contracts, and
first-launch-critical flows.

## Source Of Truth

Before writing or changing tests, read the relevant sources in this order:

1. Explicit issue scope and acceptance criteria.
2. Domain docs, specs, OpenAPI, and migration docs.
3. Existing tests for the same package or adjacent package.
4. Implementation details only after the expected behavior is understood.

Implementation may be used to identify setup, data flow, SQL behavior,
transaction boundaries, and uncovered branches. Do not treat current
implementation behavior as correct when it conflicts with specs or documented
behavior.

If implementation and documented behavior conflict, report the mismatch instead
of encoding the current behavior as expected.

## Existing Test Style

- Use Go's standard `testing` package unless the package already uses another
  local pattern.
- Do not introduce a new assertion library for a narrow test change.
- Prefer `t.Run` for scenario groups and keep assertions explicit.
- Match the surrounding package's fake names, helper style, setup flow, and
  assertion style before adding new helpers.
- Add helpers only when they remove meaningful repetition in the same test file
  or match an established local pattern.
- Keep tests behavior-focused. Do not assert private helper behavior or call
  order unless that is the stable contract.

## Test Type By Change Type

### Application And Domain Changes

Use focused Go unit tests.

Cover:

- input validation and normalization
- domain transitions and rejected transitions
- application authorization or property-scope decisions owned by the service
- repository/application error mapping
- transaction behavior when the service owns orchestration
- domain `State()` defensive-copy behavior for pointers, slices, maps, or
  JSON-like values

Prefer fakes for ports. Application and domain tests must not use Gin, concrete
database adapters, SQL setup, or external providers unless the package already
owns that boundary.

Services should be tested through the consuming package's port interfaces and
request/response contracts. Do not import concrete database adapter packages to
make an application test easier.

### Repository And SQL Changes

Use repository contract tests.

The current repository test style primarily uses
`github.com/DATA-DOG/go-sqlmock`. Prefer that style for:

- SQL shape and args
- scan behavior
- nullable fields
- JSON marshal/unmarshal behavior
- `sql.ErrNoRows` to package sentinel mapping
- `RowsAffected()` not-found/conflict semantics
- transaction begin/commit/rollback behavior

Use isolated PostgreSQL integration tests only when the behavior depends on real
PostgreSQL semantics, such as:

- schema constraints
- JSONB behavior
- partial indexes
- migration compatibility
- transaction isolation or locking behavior that `sqlmock` cannot prove

Repository tests must not assert private helper implementation unless the helper
is itself the stable contract.

### HTTP, Router, And OpenAPI Changes

Use HTTP boundary contract tests.

Cover:

- request body binding
- path/query parameter parsing
- generated OpenAPI wrapper binding errors
- shared API error schema
- authenticated principal forwarding
- property-scoped and resource-scoped authorization forwarding
- not-found versus unauthorized behavior
- route policy and OpenAPI operation parity where applicable

Match the existing router test style: use the real router with focused fake
dependencies when testing middleware, route policy, resolver, or forwarding
behavior. Do not create a separate mini-router pattern for new tests.

Handler/router fakes should capture IDs, filters, principal data, property IDs,
and resource IDs when forwarding is part of the contract.

Do not move business rules into handlers just to make tests easier.

### Migration Changes

For schema migration runner changes, use migration runner tests or dry-run/schema
checks.

Cover:

- migration ordering
- `schema_migrations` recording
- already-applied migration skip behavior
- rollback or failure behavior where practical
- isolated database setup when using PostgreSQL integration tests

Migration tests must not depend on a developer's shared local database state.

### Legacy Migration Mapping Changes

Use legacy source data, migration docs, and accepted mapping behavior as the
expected behavior.

Cover:

- mapping table writes
- already-mapped rerun/idempotency behavior
- missing prerequisite mappings
- transaction rollback on insert or mapping failure
- import report consistency
- final validation behavior for representative data

Do not invent new legacy business rules from implementation details alone.

### External Adapter Changes

Use fakes, `httptest`, emulators, or narrow seams. Do not call real external
providers in normal tests.

Cover contract-bearing behavior for:

- Firebase claim parsing and user-management error mapping
- notification provider request construction and failure handling
- storage upload URL, metadata, and missing object semantics

If an adapter cannot be tested without a design seam, keep the seam minimal and
tied directly to observable behavior.

### E2E Changes

E2E tests are opt-in and run through the repository scripts documented in
`README.md`.

Before reporting E2E environment blockers, agents must inspect the README E2E
section and the local scripts:

- `scripts/e2e_api.sh` for starting the API with E2E defaults.
- `scripts/e2e_test.sh` for running the Go E2E suite with required env vars.

Prefer the scripts over hand-written environment setup because they encode the
current expected defaults for the E2E database, Firebase Auth Emulator,
scheduler key, and E2E-only external adapter seams.

If the full E2E runtime cannot be started in the current environment, still run
the narrowest useful checks, such as package compile checks with the `e2e` build
tag, and state clearly which runtime dependency was unavailable.

### Bug Fixes

A bug fix should include a reproducing test first whenever practical.

The test should fail before the fix and pass after the fix. If a reproducing
test is not practical, explain why in the PR and provide the narrowest
alternative verification.

## PR Testing Gate

Every PR should state:

- what type of change it makes
- which tests prove the change
- which relevant package tests were run
- whether `go test ./...` was run
- why tests were not added, if applicable

Expected minimums:

- application/domain change: focused unit tests
- repository/SQL change: repository contract tests, plus PostgreSQL integration
  only when needed
- HTTP/OpenAPI change: HTTP contract tests
- migration change: migration runner, dry-run, or schema validation tests
- legacy mapping change: mapping/idempotency tests based on legacy source/spec
- external adapter change: fake, `httptest`, emulator-compatible, or narrow seam
  tests without real provider calls
- bug fix: reproducing test

## Agent Rules

AI agents must:

- read specs/docs and existing tests before using implementation details to find
  branches
- choose the narrowest test that proves the behavior
- avoid asserting private call order or helper details unless they are the
  contract
- avoid adding broad abstractions or test infrastructure for a single narrow
  case
- report ambiguity or spec/implementation mismatch before writing expected
  behavior
- run the smallest relevant test set after changes, then broader tests when risk
  warrants it
