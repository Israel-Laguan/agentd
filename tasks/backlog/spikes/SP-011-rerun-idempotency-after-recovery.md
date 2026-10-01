# SP-011: Is re-running a recovered task safe? Partial workspace state

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Time box | 1 day |
| Links | B-003, B-008, T-030, internal/queue/recovery/recover.go, internal/queue/worker/, J08 in test/e2e/journeys_recovery_test.go |

## Question

When boot recovery resets an interrupted RUNNING task to READY it is re-dispatched from the start. What does the first attempt leave behind (files written, commands run, git state, tool side effects) and does the second attempt cope with it? Is anything cleaned or reconciled before the re-run?

## Output

A short decision doc (go / no-go, with a repro or a test where the answer is "it breaks") and, if code is needed, a sized `T-` item.

- [ ] A test that kills a task mid-write (a command that creates a file then sleeps), recovers it, and records what the second attempt sees.
- [ ] A list of side effects that are not idempotent, and a rule (clean workspace, retry as is, or hand to a HUMAN task).

## Out of scope

- Changing behaviour. The spike answers the question and sizes the fix.

## Notes

- Raised during S08 planning (2026-10-01) while deciding T-030. Claims below are from reading the code, not from a failing test, so the first job is to confirm each one.
- J08 only asserts that a RECOVERY event appears; it never checks the re-run's result.
- Not yet verified whether the workspace is cleaned before the re-run.
