# B-026: SP-010 + SP-013 "size together if resurfaced" has no ticket

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready (standing ticket: fires on resurface; ticket home now exists) |
| Priority | P3 |
| Sprint | backlog (standing; from S11-open-findings) |
| Severity | minor |
| Links | S09 SP-010, S09 SP-013, `internal/queue/recovery/recover.go` |

## Symptoms

- SP-013's prose says: "If this ever bites, the cheapest real fix is a per-boot
  instance ID — the same change SP-010 would need, so the two should be sized
  together if either is resurfaced."
- Neither SP-010 nor SP-013 has a ticket tracking this conditional future work.
  It exists only in SP-013's notes.
- Both were dropped with accepted limits (~2m PID-recovery delay after a host
  reboot; no single-instance guard). The accepted limits have skip-guard tests,
  but the *fix* sizing does not have a home.

## Expected

A ticket that fires if either limit is resurfaced, so the per-boot instance ID
fix is sized once, not twice. No code change now.

## Outcome (2026-10-05)

This file is the ticket home. The conditional now has pointers from where a reader would look:
`results-history-3.md` (the SP-010/SP-013 entry) and the comments on both skip-guard tests
(`internal/queue/recovery/pid_reuse_spike_test.go`, `internal/kanban/two_daemons_spike_test.go`).
Standing, not schedulable: it carries into the next sprint folder as it stands until one of the
two skip-guard tests skips or an operator reports either limit.
