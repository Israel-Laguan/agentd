# Folder Grouping Architecture

This document describes the current folder organization for agentd's core internal packages, following the directory grouping effort that consolidated related code into focused subpackages.

Related baseline report: [folder-size-audit.md](../folder-size-audit.md).

## Overview

The codebase follows a consistent pattern of organizing internal packages into focused subpackages while maintaining backward compatibility at the root package level. Four primary packages have been organized:

1. Queue (`internal/queue`)
2. Gateway (`internal/gateway`)
3. Kanban (`internal/kanban`)
4. API (`internal/api`)

## Phase 1: Queue Grouping

Daemon/orchestration code stays in [`internal/queue`](../../internal/queue) (`daemon.go`, `loop.go`, `interface.go`, `disk_watchdog.go`, `outage_handoff.go`, …); worker/recovery/safety/planning live in subpackages. The root package re-exports public types for `cmd/agentd` via [`internal/queue/exports.go`](../../internal/queue/exports.go).

Target folders:

- `internal/queue/worker/`
- `internal/queue/recovery/`
- `internal/queue/safety/`
- `internal/queue/planning/`

Move map:

- `worker.go`, `worker_payloads.go`, `worker_support.go`, `task_runner.go` -> `internal/queue/worker/`
- `recover.go`, `prompt_recovery.go` -> `internal/queue/recovery/` (daemon outage handoff remains at `internal/queue/outage_handoff.go`)
- `breaker.go`, `semaphore.go`, `permission_detector.go`, `prompt_detector.go`, `probe.go`, `disk_stat.go` -> `internal/queue/safety/` (`disk_watchdog.go` stays with `Daemon` methods in the root package)
- `phase_planning.go`, `parameter_tuner.go` -> `internal/queue/planning/`

Import touch points:

- `internal/queue/daemon.go`
- `internal/queue/loop.go`
- `cmd/agentd/start.go`
- queue tests that reference moved files

## Phase 2: Gateway Grouping

Target folders:

- `internal/gateway/providers/`
- `internal/gateway/routing/`
- `internal/gateway/truncation/`
- `internal/gateway/correction/`

Move map:

- `openai.go`, `anthropic.go`, `ollama.go`, `horde.go`, `provider.go`, `http.go` -> `internal/gateway/providers/`
- `router.go`, `intent.go`, `scope.go` -> `internal/gateway/routing/`
- `truncate.go`, `truncate_test.go`, `truncator.go`, `truncation_strategy.go`, `truncation_head_tail.go` -> `internal/gateway/truncation/`
- `correction.go`, `contract_adapter.go` -> `internal/gateway/correction/`

Import touch points:

- `internal/api/controllers/chat.go`
- `internal/frontdesk/planner.go`
- `internal/queue/worker.go`
- tests under `internal/gateway/features`

**Status: implemented (2026).** Shared wire types live in [`internal/gateway/spec`](../../internal/gateway/spec); providers, routing, truncation, and correction are subpackages. [`internal/gateway/exports.go`](../../internal/gateway/exports.go) re-exports `AIGateway`, `Router`, `ProviderConfig`, `GenerateJSON`, truncator types/constants, provider constructors, and house-rules helpers so existing `import "agentd/internal/gateway"` call sites stay unchanged. [`internal/gateway/budget.go`](../../internal/gateway/budget.go) remains at the root package. **`contract_adapter.go`** implements `Router` methods and therefore lives in [`internal/gateway/routing/contract_adapter.go`](../../internal/gateway/routing/contract_adapter.go) (not under `correction/`), alongside JSON self-correction logic in [`internal/gateway/correction`](../../internal/gateway/correction).

## Phase 3: Kanban Grouping

Target folders:

- `internal/kanban/repo/`
- `internal/kanban/db/`
- `internal/kanban/domain/`

Move map:

- `tasks_repo.go`, `projects_repo.go`, `settings_repo.go`, `memories_repo.go`, `agent_profiles_repo.go` -> `internal/kanban/repo/`
- `db.go`, `tx.go`, `scan.go`, `rows.go`, `sql_helpers.go`, `migrations.go`, `migrations_legacy.go` -> `internal/kanban/db/`
- `dag.go`, `plan_validation.go`, `task_running.go`, `task_updates.go`, `task_retry.go`, `task_breakdown.go`, `cycle_check.go` -> `internal/kanban/domain/`

Import touch points:

- `internal/services/task_service.go`
- `internal/services/project_service.go`
- `internal/queue/*` files that call store helpers

**Status: implemented.** [`internal/kanban/db`](../../internal/kanban/db) is a full Go package containing all SQLite infrastructure: `open.go` (DB open/migrate), `tx.go` (`ImmediateTx`, `BeginImmediate`, `SQLExecutor`, `SQLQueryer`), `time.go`, `rows.go`, `sql_helpers.go` (+ `NullString`), `retry.go` (generic `RetryOnBusy`), `scan.go` / `scan_settings.go` / `scan_task.go`, `hitl_sql.go`, `tasks_queries.go`, `query_helpers.go`, `task_updates.go`, and `materialize_insert.go`. Cycle detection (`EnsureNoCycle`) was extracted to [`internal/kanban/domain/cycle_check.go`](../../internal/kanban/domain). The root `package kanban` uses a thin [`shim.go`](../../internal/kanban/shim.go) with type aliases (`immediateTx = kdb.ImmediateTx`, etc.) and `var` function forwards so all existing root files compile unchanged. Root file count dropped from 67 → 53.

### Kanban Store Methods in Root Package

By design, the `*Store` methods (e.g., `TaskStore`, `ProjectStore`, `SettingsStore`) remain in the root `internal/kanban` package rather than being moved to a subpackage. This design decision maintains the existing API surface where callers use `kanban.NewTaskStore()`, `kanban.NewProjectStore()`, etc. without needing to change their import paths. The shim layer in [`internal/kanban/shim.go`](../../internal/kanban/shim.go) provides type aliases that bridge the root package with the new subpackages, allowing internal implementation details to live in focused directories while preserving the public interface.

## Phase 4: API Grouping

Target folders:

- `internal/api/server/`
- `internal/api/tests/feature/` (optional)

Move map:

- `server.go`, `response.go`, `error_codes.go`, `doc.go` -> `internal/api/server/`
- feature tests (`api_feature_*.go`, `chat_*_test.go`) -> `internal/api/tests/feature/` (optional)
- keep existing `internal/api/controllers/` and `internal/api/sse/` structure

Import touch points:

- `cmd/agentd/wiring.go`
- `internal/api/controllers/chat_wire.go`
- tests under `internal/api` and `internal/api/sse`

**Status: implemented.** [`internal/api/server/`](../../internal/api/server) contains `NewServer`, `NewHandler`, `ServerDeps`, response helpers, and error-code aliases. The root [`internal/api/exports.go`](../../internal/api/exports.go) re-exports those symbols so `cmd/agentd` and tests keep `import "agentd/internal/api"`. Feature tests were not moved to `internal/api/tests/feature/` (optional).

## Root-Package Export/Shim Pattern

Throughout the codebase, a consistent pattern is used to maintain backward compatibility while reorganizing code into focused subpackages. This pattern involves:

1. **Type Aliases**: The root package defines type aliases that point to types in subpackages. For example, in `internal/kanban/shim.go`:
   ```go
   type immediateTx = kdb.ImmediateTx
   ```

2. **Variable Forwards**: The root package provides `var` declarations that forward to subpackage implementations:
   ```go
   var EnsureNoCycle = domain.EnsureNoCycle
   ```

3. **Function Re-exports**: Public functions from subpackages are re-exported at the root level:
   ```go
   func NewServer(deps ServerDeps) *server.Server {
       return server.NewServer(deps)
   }
   ```

4. **Dedicated Exports Files**: Some packages use a dedicated `exports.go` file to centralize all re-exports:
   - [`internal/queue/exports.go`](../../internal/queue/exports.go)
   - [`internal/gateway/exports.go`](../../internal/gateway/exports.go)
   - [`internal/api/exports.go`](../../internal/api/exports.go)

This pattern allows existing call sites to continue using `import "agentd/internal/..."` without changes, while the actual implementation lives in well-organized subpackages. New code can import directly from subpackages when needed.