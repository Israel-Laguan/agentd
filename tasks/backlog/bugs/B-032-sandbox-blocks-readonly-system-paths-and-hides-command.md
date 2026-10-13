# B-032: Sandbox rejects every absolute-path token (even `/etc/os-release`, `/dev/null`), and the violation event omits the command

| Field | Value |
| --- | --- |
| Type | bug |
| Status | done |
| Priority | P2 |
| Sprint | backlog |
| Severity | major |
| Links | internal/sandbox/sanitize.go (`validateAbsolutePathTokens`), SP-018 |

## Symptoms

Task "Inspect this machine … write OS and hardware summary to report.md" (hand-materialized, `assignee: SYSTEM`) went `READY → RUNNING → FAILED_REQUIRES_HUMAN` after 3 retries and ~593 tokens. No `report.md` was written. Every attempt logged `sandbox: directory escape attempt blocked … absolute path escapes workspace`.

## Repro

1. Materialize a plan with `start_empty_workspace: true` and a single SYSTEM task asking the worker to inspect the host.
2. Watch `GET /api/v1/tasks/<id>/events`: `SANDBOX_VIOLATION` → `RETRY` ×3 → `FAILED_REQUIRES_HUMAN`.

## Expected

- Read-only system paths (`/etc/os-release`, `/proc/*`, `/sys/*`, `/dev/null`) are usable by commands the worker runs, or the limit is documented and the task prompt tells the model not to use them.
- The violation event records which command and token tripped the check.

## Actual

- `validateAbsolutePathTokens` rejects any token that is absolute and outside the workspace. This includes `2>/dev/null`, `cat /etc/os-release` and `cat /proc/cpuinfo`.
- `SANDBOX_VIOLATION` payload is only "directory escape attempt blocked", and the `RETRY` payload is the generic error. The offending command is not in the event or the log, so it can't be debugged from the board.
- The model isn't told why a command was refused, so it presumably repeats a similar command (unverified; the command text wasn't captured).

## Notes

- Related decision: SP-018 (read-only host-introspection allowlist).
- Log the command (truncated) in the SANDBOX_VIOLATION event, and feed the violation reason back to the model on retry.
- Found during the first manual end-to-end test, 2026-10-13.
