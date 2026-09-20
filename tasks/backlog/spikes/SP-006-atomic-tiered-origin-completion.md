# SP-006: Atomic tiered-origin completion

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Time box | 1 day |
| Links | docs/tiered-execution.md, internal/queue/worker/worker_tiered_origin.go |

## Question

How can an escalated tiered origin be completed atomically while it is BLOCKED,
without exposing a transient READY state that another dispatcher can claim and
re-block using a stale conflict verdict?

## Output

Concrete store/API design and a go/no-go recommendation, including optimistic
concurrency behavior and coverage for concurrent dispatch/completion.

- [ ] Identify the smallest atomic persistence operation for BLOCKED -> COMPLETED.
- [ ] Prototype or document the transaction and state-machine changes required.
- [ ] Add a focused concurrency test plan covering stale dispatcher claims.
- [ ] Record the go/no-go decision and implementation follow-up.

## Out of scope

- Production implementation of the atomic completion path.
- Changes to unrelated tiered retry or escalation limits.

## Notes

The current completion helper walks BLOCKED -> READY -> RUNNING before calling
UpdateTaskResult, leaving a claimable window between state transitions.
