# B-029: Two conditional deferrals are tracked only in prose

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready |
| Priority | P3 |
| Sprint | backlog (standing; from S11-open-findings) |
| Severity | minor (process) |
| Links | S10 retro "Carry into S11", S11 README "Explicitly out of scope", S08 retro carry-over, `docs/architecture/recovery-rerun.md` (Known limits) |

## Symptoms

Same shape as B-026 and B-027: a future item with a stated re-entry condition and no ticket.

1. **Process-group overlap in the re-run.** `recovery-rerun.md` (Known limits): recovery does not
   verify or terminate the killed attempt's process group, so a command that outlives its daemon can
   still be writing while the re-run starts, and the opt-in workspace reset does not stop it. S10's
   retro and S11's README both say "measure only if a journey needs it". Nothing measures it.
2. **Deterministic ledger event IDs.** S08's accepted limit: a lock held longer than ~30 s still drops
   a `TOKEN_USAGE` row. The follow-up, deterministic event IDs, is conditional on SQLite's BUSY
   guarantee ever stopping to hold. It is named in the S08 retro, the S10 README and the S11 README
   and is a ticket in none of them.

## Expected

A standing ticket each, with its re-entry condition and no code now.

## Outcome (2026-10-05)

This file is the ticket home for both:

| Item | Re-entry condition | First step when it fires |
| --- | --- | --- |
| Process-group overlap | A journey, a bug report, or a reviewer shows a re-run racing a surviving command | Measure first: start a command that outlives its daemon, restart, and record what writes after the re-run begins (the SP-011 method) |
| Deterministic ledger event IDs | A `TOKEN_USAGE` row is observed dropped under a lock held past the BUSY timeout, or SQLite's BUSY handling changes | Size the ID scheme against `internal/kanban` event inserts; see B-001's retry work in S08 for what already bounds it |

`recovery-rerun.md`'s Known limits section now names this ticket. Standing, like B-026 and B-027.
