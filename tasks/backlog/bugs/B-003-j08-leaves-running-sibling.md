# B-003: J08 recovery leaves a sibling task RUNNING (needs investigation)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P3 |
| Sprint | backlog |
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

Unconfirmed whether these are real stuck tasks or the still-executing
re-dispatch caught mid-flight. Not yet checked which task each one is.

## Notes

- J08 is also about 90% of suite wall-clock (about 124s of about 250s) because of the recovery wait.
