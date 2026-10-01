# SP-012: Provider outage: how long until work resumes after connectivity returns?

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Time box | 1 day |
| Links | J09 (breaker profile), internal/queue/safety/, devenv/mockllm (scenarios: error, latency), docs/testing/journeys.md |

## Question

During an internet or provider outage the daemon stays alive: tasks fail, the breaker opens after 3 worker failures and a HUMAN handoff task is created. When the provider comes back, how long until the breaker closes and tasks resume, what happens to the failed tasks and the HUMAN task, and does anything need manual action?

## Output

A short decision doc (go / no-go, with a repro or a test where the answer is "it breaks") and, if code is needed, a sized `T-` item.

- [ ] A journey sketch: all providers dead → breaker OPEN → provider healthy again → measured time to CLOSED and first task COMPLETED.
- [ ] Whether in-flight and failed tasks are retried automatically or stay FAILED_REQUIRES_HUMAN, and what the operator has to do.

## Out of scope

- Changing behaviour. The spike answers the question and sizes the fix.

## Notes

- Raised during S08 planning (2026-10-01) while deciding T-030. Claims below are from reading the code, not from a failing test, so the first job is to confirm each one.
- J09 only proves the trip (OPEN + HUMAN child), not the recovery. The mock supports error scenarios per request (T-028), so no new mock work is expected.
- The startup race fixed in S07 showed the breaker can hold closed-state for 5 minutes; check that figure.
