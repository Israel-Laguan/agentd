# Phase 1 Skeleton Contract

This document defines the v1 Phase 1 baseline for `agentd` and acts as the acceptance contract for foundational hardening.

## Scope

Phase 1 covers only the foundational substrate:

- Go project layout and bootstrap (`cmd`, `internal`, module wiring).
- Domain entities and enums in `internal/models`.
- SQLite schema + migrations in `internal/kanban`.
- Store-level CRUD and lifecycle guards for projects/tasks.

Later-phase capabilities (gateway fallback, sandbox execution, queue orchestration, SSE, CLI UX polish) are explicitly out of this contract.

## Required Data Model

The minimum durable entities are:

- `projects`: identity, intent (`original_input`), workspace path, status, timestamps.
- `tasks`: identity, project foreign key, title/description, assignee, lifecycle state, runtime metadata, timestamps.
- `task_relations`: parent/child edges and relation type.
- `events`: immutable timeline entries for task/project activity.
- `settings`: runtime key/value configuration.

## Task Lifecycle Contract

Task state is constrained to:

`PENDING`, `READY`, `QUEUED`, `RUNNING`, `BLOCKED`, `COMPLETED`, `FAILED`, `FAILED_REQUIRES_HUMAN`, `IN_CONSIDERATION`.

Transitions are validated at the model/store layer. Invalid transitions must fail with `ErrInvalidStateTransition`. Concurrent writes must fail deterministically with optimistic-lock/state-conflict errors.

## Schema + Migration Contract

- Fresh bootstrap must initialize schema and set `settings.schema_version` to current.
- Migrations must preserve existing task rows and enforce current constraints.
- Foreign keys and check constraints are mandatory.
- `task_relations.relation_type` supports `BLOCKS`, `SPAWNED_BY`, and `DEPENDS_ON`.

## Store CRUD Contract

The store must guarantee:

- Materializing a valid plan inserts one project, task rows, and valid relations atomically.
- Claiming ready tasks is atomic under concurrency.
- Updating task state/result respects lifecycle guards and optimistic locking.
- Not-found and invalid-transition behavior is deterministic and test-covered.

## Verification Gate

Phase 1 is complete only when:

- `internal/models` tests pass for enum/state invariants.
- `internal/kanban` tests pass for schema/migration/store invariants.
- README and architecture docs point to this contract as the baseline.
