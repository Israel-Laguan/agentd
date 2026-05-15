# GUARDRAILS.md

Safety protocol for autonomous agents working in this repository.

## Meta

Created: 2026-05-09
Total Signs: 10
Protocol: [guardrails.md](https://guardrails.md/#protocol)

## Subagent delegation tools

Subagents do not receive `delegate` or `delegate_parallel` unless those tool names are explicitly listed in the subagent’s allowed-tools configuration (parent workers in agentic mode always have them). Delegation from subagents is intentionally opt-in.

---

## SIGN #1: models layer boundary

**Trigger:** Writing or modifying code in `internal/models/`
**Instruction:** `internal/models` must never import from any outward layer (`internal/api`, `internal/services`, `internal/kanban`, `internal/config`, `internal/bus`, `internal/memory`, `internal/gateway`, `internal/queue`, `internal/sandbox`). It contains only pure data definitions, enums, sentinel errors, and interfaces. If you need a function from an outward layer, move the logic outward — never import it inward.
**Reason:** Inward-facing dependency direction is a core architectural invariant. `depguard` in golangci-lint enforces this at build time. Violating it creates circular dependencies and breaks the domain layer separation described in `docs/architecture.md`.
**Provenance:** Architectural invariant from phase 1 contract.

---

## SIGN #2: file and function size limits

**Trigger:** Creating or editing any Go file
**Instruction:**
- Files must not exceed 300 lines; test files (*_test.go) allow 500 lines and paths matching `docs/**` allow 400 lines (enforced by scripts/check_loc.py).
- Functions must not exceed 60 lines and 40 statements (enforced by `funlen` / `revive`).
- Cyclomatic complexity must not exceed 15 (enforced by `cyclop`).
- If you hit these limits, split by behavior — extract helpers, move logic to a new file, or introduce a focused package.

**Reason:** Large files accumulate hidden coupling; large functions do too many things. These limits keep code navigable and testable. See `docs/guardrails.md` for the full rationale.
**Provenance:** Established repository guardrails, enforced by `make lint` and `make loc`.

---

## SIGN #3: no blanket linter suppressions

**Trigger:** About to add `//nolint`, `//nolint:funlen`, or `//revive:disable`
**Instruction:** Never add blanket linter suppressions. Fix the code to pass lint. The only accepted `//nolint:errcheck` exceptions are `defer listener.Close()` and `defer resp.Body.Close()` patterns already established in the codebase — and only when there is no meaningful recovery path. Never suppress `funlen`, `cyclop`, `depguard`, or `revive` rules.
**Reason:** Suppressions hide technical debt and make guardrails unenforceable. Two existing `//nolint:funlen` instances in feature test step files (`internal/kanban/kanban_feature_test.go`, `internal/queue/queue_feature_steps_test.go`) are legacy — do not add more.
**Provenance:** Repository engineering practices.

---

## SIGN #4: context as first parameter

**Trigger:** Writing any public function or interface method that performs I/O, database calls, or HTTP requests
**Instruction:** `context.Context` must always be the first parameter: `func(ctx context.Context, ...)`. This applies to interface definitions in `internal/models/interfaces.go`, store methods, gateway methods, service methods, and provider implementations. The only exception is pure functions (e.g., `TruncateToBudget`) that perform no I/O.
**Reason:** Consistent context propagation is required for cancellation, tracing, and timeout enforcement throughout the worker pipeline and API handlers.
**Provenance:** Go project convention, followed consistently across all 42 `KanbanStore` methods and all provider implementations.

---

## SIGN #5: sentinel errors and error wrapping

**Trigger:** Defining or returning errors
**Instruction:**
1. Define sentinel errors as `var ErrXxx = errors.New("...")` in a package-level `errors.go` file (see `internal/models/errors.go` for the canonical pattern).
2. Wrap errors with `fmt.Errorf("%w: context", ErrSentinel)`. Never use `%v` with sentinels.
3. Check errors with `errors.Is(err, ErrSentinel)`, never string comparison.
4. Do not define sentinel errors inline or in non-`errors.go` files.

**Reason:** Consistent error handling enables `errors.Is` checks at API boundaries (controllers map sentinels to HTTP status codes) and circuit breaker logic (`ErrLLMUnreachable`, `ErrLLMQuotaExceeded`).
**Provenance:** Established error handling pattern used in 290+ call sites across the codebase.

---

## SIGN #6: compile-time interface checks

**Trigger:** Implementing an interface defined in another package
**Instruction:** Add a compile-time interface satisfaction check at the package level: `var _ Package.Interface = (*Impl)(nil)`. Place it in the same file as the implementing type's definition. See examples: `internal/kanban/store.go:16`, `internal/gateway/routing/router.go:29`, `internal/sandbox/executor.go:33`.
**Reason:** Without compile-time checks, an implementation can silently drift from its interface. These assertions catch mismatches at build time, not runtime.
**Provenance:** Used for all 13 interface implementations in the codebase (Store, Router, Executor, Adapter, etc.).

---

## SIGN #7: import ordering

**Trigger:** Writing or editing imports in any Go file
**Instruction:** Group imports in three sections separated by blank lines:
1. Standard library (`context`, `errors`, `fmt`, `net/http`, ...).
2. External dependencies (`github.com/...`, `modernc.org/...`).
3. Internal packages (`agentd/internal/...`).

Do not mix groups. Do not use aliases unless there is a name collision.
**Reason:** Consistent import ordering makes diffs cleaner and dependencies scannable. This convention is followed in every Go file in the codebase.
**Provenance:** Project-wide convention (see any `.go` file for reference).

---

## SIGN #8: package documentation

**Trigger:** Creating a new package under `internal/`
**Instruction:** Create a `doc.go` file in the package with a `// Package <name> ...` comment. Describe what the package owns and — importantly — what it does NOT own. Reference other packages for cross-cutting concerns. See `internal/services/doc.go` for the canonical example.
**Reason:** Package boundaries are a core architectural concept in this project. Doc comments make those boundaries explicit for agents and humans. Without them, it is easy to place logic in the wrong layer.
**Provenance:** Every existing package has a `doc.go` file.

---

## SIGN #9: tests must accompany behavior changes

**Trigger:** Adding or modifying behavior in any `internal/` package
**Instruction:**
1. Write unit tests alongside behavior changes in `*_test.go` files. Use table-driven tests for multi-case scenarios.
2. Use `t.Helper()` in test helper functions.
3. For flows and integrations, add Godog BDD `.feature` files with step definitions.
4. Tests must pass with the race detector: `go test -race ./...`.
5. Do not skip writing tests. If the change is documentation-only, state that explicitly.

**Reason:** The project has 28 test packages passing with `-race`. Race detector failures indicate real concurrency bugs in this system (workers, queue dispatch, SQLite access).
**Provenance:** Repository quality gate (`make check` runs `go test -race ./...`).

---

## SIGN #10: read architecture docs before structural changes

**Trigger:** Making changes that cross package boundaries, add new packages, modify the worker/queue/gateway pipeline, or touch `internal/models/interfaces.go`
**Instruction:** Before writing code, read the relevant architecture docs:
- `docs/architecture.md` — system nodes, data flows, architectural invariants.
- `docs/architecture-flows.md` — Manager's Loop, Memory Recall extended flows.
- `docs/frontdesk.md` — chat intake decision flow, package boundaries, interface seams.
- `docs/guardrails.md` — size limits, layer boundaries, pre-commit checklist.

If your change affects the Board/Store layer, task lifecycle states, or the worker execution cycle, read the relevant failure analysis sections to understand the existing safety mechanisms.
**Reason:** This system has carefully designed invariants (optimistic locking, sandbox jailing, token budgets, circuit breakers, context truncation) that prevent catastrophic failure modes. Changing one component without understanding its invariants can introduce "The Gutter" — recursive failure loops documented in `docs/architecture.md`.
**Provenance:** Architectural invariants documented in `docs/architecture.md` and `docs/phase1-skeleton.md`.
