# SP-006: Atomic tiered-origin completion

| Field | Value |
| --- | --- |
| Type | spike |
| Status | done (PR 1) |
| Priority | P1 |
| Sprint | S06-stabilize-observe |
| Time box | 1 day |
| Links | docs/tiered-execution.md, internal/queue/worker/worker_tiered_origin.go |

## Question

How can an escalated tiered origin be completed atomically while it is BLOCKED,
without exposing a transient READY state that another dispatcher can claim and
re-block using a stale conflict verdict?

## Output

Concrete store/API design and a go/no-go recommendation, including optimistic
concurrency behavior and coverage for concurrent dispatch/completion.

- [x] Identify the smallest atomic persistence operation for BLOCKED -> COMPLETED.
- [x] Prototype or document the transaction and state-machine changes required.
- [x] Add a focused concurrency test plan covering stale dispatcher claims.
- [x] Record the go/no-go decision and implementation follow-up.

## Decision: GO — `CompleteTieredOrigin`

Implemented in S06 PR 1 (not just prototyped — the change is small and the
race is real):

- New single-statement write `CompleteTieredOriginState` (`UPDATE ... WHERE
  id = ? AND updated_at = ? AND state IN (BLOCKED, READY, RUNNING)`) plus the
  standard result side effects (unlock children, unblock parents, RESULT
  event) in one transaction. Exposed as `kanban.Store.CompleteTieredOrigin`,
  mirrored on `testutil.FakeKanbanStore`, and added to the `KanbanStore`
  interface.
- `completeTieredOrigin` / `failTieredOrigin` now resolve through that one
  call. The old BLOCKED→READY→RUNNING ladder is deleted. The generic
  `UpdateTaskState` machine still rejects BLOCKED/READY → COMPLETED — only
  this path may bypass it.
- Optimistic concurrency is preserved: a stale version (dispatcher claimed
  the origin mid-flight, or a concurrent resolution landed) reports
  `ErrStateConflict` and the worker leaves the origin alone instead of
  forcing a verdict.
- Coverage: `internal/kanban/tiered_origin_test.go` (atomic BLOCKED→COMPLETED
  with no claimable residue, stale-version conflict, READY/RUNNING sources)
  and `internal/queue/worker/worker_tiered_origin_test.go` (atomic complete,
  atomic fail, deterministic stale-dispatcher race via a version-bumping
  store wrapper).

## Out of scope

- Changes to unrelated tiered retry or escalation limits.

## Notes

The previous completion helper walked BLOCKED -> READY -> RUNNING before
calling UpdateTaskResult, leaving a claimable window between state
transitions.
