# Folder Grouping Roadmap

This roadmap turns the folder-size audit into an execution plan for safe, incremental directory grouping.

Latest baseline report: [`docs/folder-size-audit.md`](folder-size-audit.md).

## Execution Order

1. Queue (`internal/queue`)
2. Gateway (`internal/gateway`)
3. Kanban (`internal/kanban`)
4. API (`internal/api`)

Run `go test ./...` after each phase before moving to the next one.

## Phase 1: Queue Grouping

**Status: implemented.** Daemon/orchestration code stays in [`internal/queue`](../internal/queue) (`daemon.go`, `loop.go`, `interface.go`, `disk_watchdog.go`, `outage_handoff.go`, …); worker/recovery/safety/planning live in subpackages. The root package re-exports public types for `cmd/agentd` via [`internal/queue/exports.go`](../internal/queue/exports.go).

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

Expected import touch points:

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

Expected import touch points:

- `internal/api/controllers/chat.go`
- `internal/frontdesk/planner.go`
- `internal/queue/worker.go`
- tests under `internal/gateway/features`

**Status: implemented (2026).** Shared wire types live in [`internal/gateway/spec`](../internal/gateway/spec); providers, routing, truncation, and correction are subpackages. [`internal/gateway/exports.go`](../internal/gateway/exports.go) re-exports `AIGateway`, `Router`, `ProviderConfig`, `GenerateJSON`, truncator types/constants, provider constructors, and house-rules helpers so existing `import "agentd/internal/gateway"` call sites stay unchanged. [`internal/gateway/budget.go`](../internal/gateway/budget.go) remains at the root package. **`contract_adapter.go`** implements `Router` methods and therefore lives in [`internal/gateway/routing/contract_adapter.go`](../internal/gateway/routing/contract_adapter.go) (not under `correction/`), alongside JSON self-correction logic in [`internal/gateway/correction`](../internal/gateway/correction).

## Phase 3: Kanban Grouping

Target folders:

- `internal/kanban/repo/`
- `internal/kanban/db/`
- `internal/kanban/domain/`

Move map:

- `tasks_repo.go`, `projects_repo.go`, `settings_repo.go`, `memories_repo.go`, `agent_profiles_repo.go` -> `internal/kanban/repo/`
- `db.go`, `tx.go`, `scan.go`, `rows.go`, `sql_helpers.go`, `migrations.go`, `migrations_legacy.go` -> `internal/kanban/db/`
- `dag.go`, `plan_validation.go`, `task_running.go`, `task_updates.go`, `task_retry.go`, `task_breakdown.go`, `cycle_check.go` -> `internal/kanban/domain/`

Expected import touch points:

- `internal/services/task_service.go`
- `internal/services/project_service.go`
- `internal/queue/*` files that call store helpers

**Status: partial.** [`internal/kanban/db/schema.sql`](../internal/kanban/db/schema.sql) holds the embedded schema (import path in [`internal/kanban/db.go`](../internal/kanban/db.go): `//go:embed db/schema.sql`). DAG and draft-plan normalization live in [`internal/kanban/domain`](../internal/kanban/domain) (`ValidateDAG`, `NormalizeDraftPlan`, `ValidateTaskCap`). **`internal/kanban/repo`** and a dedicated Go package **`internal/kanban/db`** (moving `tx`, `scan`, migrations, etc. out of package `kanban`) are not done yet—`*Store` methods remain in package `kanban`.

## Phase 4: API Grouping

Target folders:

- `internal/api/server/`
- `internal/api/tests/feature/` (optional)

Move map:

- `server.go`, `response.go`, `error_codes.go`, `doc.go` -> `internal/api/server/`
- feature tests (`api_feature_*.go`, `chat_*_test.go`) -> `internal/api/tests/feature/` (optional)
- keep existing `internal/api/controllers/` and `internal/api/sse/` structure

Expected import touch points:

- `cmd/agentd/wiring.go`
- `internal/api/controllers/chat_wire.go`
- tests under `internal/api` and `internal/api/sse`

**Status: implemented.** [`internal/api/server/`](../internal/api/server) contains `NewServer`, `NewHandler`, `ServerDeps`, response helpers, and error-code aliases. The root [`internal/api/exports.go`](../internal/api/exports.go) re-exports those symbols so `cmd/agentd` and tests keep `import "agentd/internal/api"`. Feature tests were not moved to `internal/api/tests/feature/` (optional).

## Migration Checklist (Per Phase)

- Create target folders and move only a coherent slice (do not mix domains in one commit).
- Keep package names stable where possible to minimize code churn.
- Update imports and compile with `go test ./...`.
- Fix any broken `_test.go` references and rerun `go test ./...`.
- Update architecture/docs references if any moved paths are linked.
- Stop and revert only the in-flight phase if tests fail repeatedly.
