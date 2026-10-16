# B-027: Real-browser e2e tier — accepted gap with no ticket home

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready (standing ticket: a product decision, never "done") |
| Priority | P3 |
| Sprint | backlog (standing; from S11-open-findings) |
| Severity | minor (docs) |
| Links | `docs/testing/journeys.md` (J06 gap note), S09 retro carry-over, S10/S11 README out-of-scope |

## Symptoms

- A real-browser tier (Playwright) is the accepted gap that J06's component tests
  leave open: CSS layout and click-through are unproven.
- It is recorded as "deliberately not backlogged" in the S09 retro, S10 README
  out-of-scope, and S11 README out-of-scope — three places agreeing, but no ticket.
- The deferral is a product decision, not a work item, so it will never be "done";
  without a ticket it has no home to point at when someone asks "where is the
  browser tier tracked?"

## Expected

This bug is the ticket home. It stays `ready` / P3 indefinitely unless the product
decision changes. No code change now; the S11 README out-of-scope line should point
here.

## Outcome (2026-10-05)

This file is the ticket home, and `journeys.md` (the J06 gap note) now names B-027 as where the gap
is tracked. S10's and S11's out-of-scope lines already point here. Standing, as the ticket says.
