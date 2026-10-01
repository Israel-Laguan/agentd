# B-001: Writes still drop when SQLite stays busy past the timeout

| Field | Value |
| --- | --- |
| Type | bug |
| Status | fixed |
| Priority | P2 |
| Sprint | backlog (fixed in S08, T-031) |
| Severity | minor (reproduced at 5% loss; impact is silent budget drift) |
| Links | docs/testing/journeys.md ("SQLite drops events under contention"), internal/kanban/db/open.go, internal/kanban/db/retry.go |

## Symptoms

The per-connection pragma fix gives every connection a 5s `busy_timeout`, and
4 consecutive suite runs on one accumulating stack logged zero `SQLITE_BUSY`.
A write that still loses after that window is logged and dropped by the
caller (for example `failed to persist token usage`), so a `TOKEN_USAGE` row
can be lost under heavier contention than the suite generates.

## Repro

**Reproduced 2026-10-01** (SP-009). No repro on the ordinary suite, as expected;
it needs the lock held past the 5s `busy_timeout`. See
`internal/kanban/busy_write_spike_test.go`:

```
RESULT unwrapped attempts=160 ok=152 failed=8 elapsed=8.048s first_failure=4.713s
```

8 concurrent workers x 20 ledger counter writes against a file-backed DB while a
second connection holds the write lock for 8s: **8 of 160 writes dropped, 5%.**
With the fix: 160/160, zero loss.

The drops are clean losses, not partial or double-counted writes — the durable
counter exactly equalled the successful write count. That is what makes retrying
safe (see below).

## Expected

A product decision on what a write does when SQLite stays busy: retry longer,
queue and flush, or fail the task loudly. Ledger writes (`TOKEN_USAGE`) should
not be silently lost.

## Notes

- **The premise in the note below was wrong, and this is the actual finding.**
  `RetryOnBusy` retries 6 times with 5ms doubling backoff, ~150ms total, which
  *looks* shorter than the 5s `busy_timeout` it sits behind. But that 150ms is
  only the backoff *between* attempts — each attempt independently gets the
  driver's own 5s window. The real tolerance is **6 x 5s ~= 30s**, measured:
  a lock held 8s and 25s loses nothing, 32s loses writes again. So `RetryOnBusy`
  was always worth keeping; it just had no test proving why.

- Root cause was narrower than "no retry beyond timeout": **the three ledger
  writes were the only unwrapped ones in the package.** `AddTokenUsage`,
  `AddUsageDetails` and `AppendEvent` had no retry wrapper while ~30
  neighbouring call sites did, including `AddComment` in the very same file as
  `AppendEvent`. An oversight, not a design decision.

- The `TOKEN_USAGE` **event** insert was worse than the counter: it went through
  `Worker.Emit`, which discarded the sink error with `_ =` and did not even log
  it. A lost ledger row left no trace at all.

- **Why retrying is safe even though neither write is idempotent.** The counter
  is `UPDATE ... SET token_usage = token_usage + ?` (a replay would
  double-count) and the event mints a fresh `uuid.NewString()` per attempt (a
  replay would duplicate). Retry is still correct because SQLite guarantees a
  write that returned `SQLITE_BUSY` did not commit — there is no window where
  the row landed and the error was reported anyway. The measured
  counter==successful-writes result confirms it empirically. This holds *only*
  for BUSY/LOCKED; `isSQLiteBusy` matches exactly those codes.

- **Fixed 2026-10-01** (T-031): all three writes wrapped in `RetryOnBusy`, and
  `Emit` logs a dropped event at Error instead of discarding it. Accepted limit:
  a lock held longer than ~30s still drops a row — at that point the daemon is
  not making progress anyway, and blocking the agentic loop for 30s per write is
  worse. Decision and measurements in
  `tasks/sprints/S08-reliability-and-dev-speed/spikes/SP-009-busy-write-decision.md`.
