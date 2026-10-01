# SP-013: Nothing stops two daemons from sharing one AGENTD_HOME

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P3 |
| Sprint | backlog |
| Time box | 0.5 day |
| Links | T-030, internal/queue/daemon.go, cmd/agentd, internal/kanban/db/open.go |

## Question

No lock file or single-instance check was found in `internal` or `cmd`. What happens if two daemons run on one home (shared SQLite file, same queue)? Do they double-dispatch a task, fight over heartbeats, or reset each other's tasks via reconcile? Should startup refuse to run a second instance?

## Output

A short decision doc (go / no-go, with a repro or a test where the answer is "it breaks") and, if code is needed, a sized `T-` item.

- [ ] A run of two daemons on one home with a few tasks, recording double-dispatch and false recoveries.
- [ ] Recommendation: advisory lock on the home (flock), a documented unsupported setup, or nothing.

## Out of scope

- Changing behaviour. The spike answers the question and sizes the fix.

## Notes

- Raised during S08 planning (2026-10-01) while deciding T-030. Claims below are from reading the code, not from a failing test, so the first job is to confirm each one.
- Found by grep for flock / pidfile / "already running" with no hits; confirm by reading the daemon start path before running anything.
- Relevant to T-030's safety argument: the narrowed boot fix stays correct with two daemons only if their PIDs differ.
