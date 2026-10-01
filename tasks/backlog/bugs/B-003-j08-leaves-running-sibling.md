# B-003: J08 recovery leaves a sibling task RUNNING (needs investigation)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | closed (not a bug) |
| Priority | P3 |
| Sprint | backlog (closed in S08, T-030) |
| Severity | minor |
| Links | test/e2e/journeys_recovery_test.go, docs/testing/journeys.md (J08) |

## Symptoms

Across 4 consecutive suite runs the board accumulated 0 -> 3 RUNNING tasks
(about one per J08 run) plus one FAILED_REQUIRES_HUMAN. J08 itself passes.

## Repro

Run `make test-e2e` repeatedly on one stack, then count tasks by state.

## Expected

After restart recovery, no task from the killed run stays RUNNING.

## Actual

**Resolved 2026-10-01 (T-030): these were the still-executing re-dispatch caught
mid-flight, not stuck tasks.** After the B-008 fix, 4 consecutive e2e runs on one
stack left exactly one RUNNING task at the end of run 4: a `SLOW_TASK J08` from
project `j08-kznz6w` with `os_process_id=1`. Its event log shows live `LOG_CHUNK
tick N` events and a `RETRY` ("execution timed out: no output within limit") while
it re-executed, and it reached a terminal state on its own ~90s later with no
intervention, leaving 0 RUNNING. J08's own test comment had already said as much.
Evidence in `docs/testing/results.md`.

## Notes

- J08 was about 90% of suite wall-clock (about 124s of about 250s) because of the recovery wait. That is fixed: recovery is now 0s and J08 runs in about 5.5s, with the suite down to about 195s from about 295s.
