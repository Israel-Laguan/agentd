# B-001: Writes still drop when SQLite stays busy past the timeout

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Severity | minor |
| Links | docs/testing/journeys.md ("SQLite drops events under contention"), internal/kanban/db/open.go, internal/kanban/db/retry.go |

## Symptoms

The per-connection pragma fix gives every connection a 5s `busy_timeout`, and
4 consecutive suite runs on one accumulating stack logged zero `SQLITE_BUSY`.
A write that still loses after that window is logged and dropped by the
caller (for example `failed to persist token usage`), so a `TOKEN_USAGE` row
can be lost under heavier contention than the suite generates.

## Repro

No repro on the current suite. Needs a load test that holds the write lock
past 5s (many concurrent workers plus a long transaction).

## Expected

A product decision on what a write does when SQLite stays busy: retry longer,
queue and flush, or fail the task loudly. Ledger writes (`TOKEN_USAGE`) should
not be silently lost.

## Notes

- `RetryOnBusy` retries 6 times with 5ms doubling backoff, about 150ms total, which is shorter than the 5s `busy_timeout` it now sits behind. Check whether it still adds anything.
