# B-008: Boot reconcile never recovers interrupted tasks when agentd is PID 1

| Field | Value |
| --- | --- |
| Type | bug |
| Status | fixed |
| Priority | P2 |
| Sprint | S08-reliability-and-dev-speed |
| Severity | major |
| Links | internal/queue/recovery/recover.go, internal/queue/worker/worker.go (`MarkTaskRunning`), test/e2e/journeys_recovery_test.go (J08), docs/testing/journeys.md (J08), B-003 |

## Symptoms

After an unclean kill, a task that was RUNNING stays RUNNING for the full
stale-heartbeat window (observed exactly 2m0s) instead of being reset at boot.
Any container deployment is affected, not only the devenv.

## Repro

`make test-e2e` and read J08's logged time-to-recovery, or: start the default
stack, materialize a `SLOW_TASK` task, wait for RUNNING, `podman kill` the
agentd container, restart it, and watch the task.

## Expected

`queue.BootReconcile` resets the interrupted task to READY at boot.

## Actual

`MarkTaskRunning` stamps `os.Getpid()`. In a container agentd is PID 1 before
and after the restart, so `BootReconcile`'s PID probe sees the owner as alive
and skips the task. Only the stale-heartbeat sweep recovers it.

## Notes

- **Fixed 2026-10-01** (T-030): `BootReconcile` now drops its own PID from the alive set before reconciling. Boot runs before any worker starts, so a RUNNING task stamped with our own PID cannot be ours. Only our own PID is exempted, so a task owned by a different live daemon sharing the home still goes through the liveness probe. No schema change. J08: `recovered 0s after restart`, was 2m0s. See `docs/testing/results.md`.
- Was the suspected cause of B-003; B-003 is now closed as not-a-bug (the leftover was the still-executing re-dispatch caught mid-flight).
- Candidate fix (2) as originally written ("treat every RUNNING task as orphaned") was **narrowed** rather than taken as-is: nothing enforces one daemon per home (no lock file, no single-instance check), so blanket-resetting would wrongly reset a second daemon's live tasks. Accepted residual limit: two daemons in separate PID namespaces both at PID 1 sharing one home would still collide; the per-boot instance ID (1) is the follow-up if multi-daemon homes are ever supported.
