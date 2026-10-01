# B-008: Boot reconcile never recovers interrupted tasks when agentd is PID 1

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready |
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

- Suspected cause of B-003 (RUNNING sibling left after J08). Re-check B-003 after this is fixed.
- Candidate fixes: (1) a per-boot instance ID stored with the running mark and compared at boot; (2) at boot, before any worker starts, treat every RUNNING task as orphaned. Pick in T-030; (2) is smaller but assumes one daemon per home.
