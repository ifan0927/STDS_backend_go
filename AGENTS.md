# AGENTS.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.
All the conversations with user are in Traditional Chinese, All the coding comment are in English.
**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.
**Trivial Task** determine if a task is trivial or not , if it is trivial, ask user for approval , all trivial tasks just need to follow the guidelines rule 3 to 4 and project-specific guidelines.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---
## Project-Specific Guidelines

- Use GoLang
- Use Gin
- All the coding style follow the idiomatic GoLang style
- Gin Error handling should follow these rules:
  - Check `internal/shared/apperr/common.go` first for reusable cross-cutting
    errors.
  - If no common error matches, define and return a domain-specific or use-
    case-specific `apperr.Error` in the owning package.
  - Do not let raw `error` values flow into Gin error middleware unless they
    are intended to become `INTERNAL_SERVER_ERROR`.
  - Use `INTERNAL_SERVER_ERROR` only as the final fallback for unmapped
    unexpected failures.
- Tests should follow these rules:
  - Prefer Go standard library testing with `t.Run` for multiple scenarios of
    the same behavior.
  - Keep assertions explicit and behavior-focused; do not introduce a new test
    framework unless the task requires it.
  - Add comments in tests only when fixture setup or legacy business context is
    not obvious from the test name and code.
  - When changing behavior, update or add the narrowest test that proves the
    expected outcome and run the relevant tests.

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
