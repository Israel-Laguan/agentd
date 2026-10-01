# SP-010: PID reuse after a host reboot delays task recovery

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P3 |
| Sprint | backlog |
| Time box | 0.5 day |
| Links | B-008, T-030, internal/queue/recovery/recover.go, internal/queue/safety/probe.go (`GopsutilProbe` lists every host PID), internal/kanban/tasks_repo.go (`ReconcileGhostTasks`) |

## Question

After a power outage or host reboot, can an unrelated process hold a dead daemon's old PID so that `BootReconcile` sees the task owner as alive and skips it? How long until the stale-heartbeat sweep (2 min) recovers it, and is that acceptable?

## Output

A short decision doc (go / no-go, with a repro or a test where the answer is "it breaks") and, if code is needed, a sized `T-` item.

- [ ] A Go test where the owner PID belongs to a live, unrelated process: confirm the task is skipped at boot and recovered only by the sweep.
- [ ] Recommendation: accept the 2 min bound, or add a per-boot instance ID / process start-time check (schema change after v19).

## Out of scope

- Changing behaviour. The spike answers the question and sizes the fix.

## Notes

- Raised during S08 planning (2026-10-01) while deciding T-030. Claims below are from reading the code, not from a failing test, so the first job is to confirm each one.
- T-030's narrowed fix (reset tasks owned by the daemon's own PID) does not cover this case. It is recorded there as a known limit.
- Bare-metal reboots hit this more than containers, where PID 1 collides instead.
