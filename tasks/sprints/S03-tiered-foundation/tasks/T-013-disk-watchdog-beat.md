# T-013: Beat 2.3 — disk/resource watchdog demo (PR-C)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Parent | US-003 (close Phase 2) |
| Estimate | M |
| PR | **PR-C** — ≤10 files / <600 LOC |
| Links | [product-plan Phase 2.3](../../../docs/product-plan.md), [harness-reliability.md](../../../docs/harness-reliability.md) |

## Goal

Prove product-plan Phase 2.3: a disk/resource crunch surfaces a durable event or task — no silent death. Docs + runnable demo + tests, mirroring the Beat 1/2 pattern (S02 PR-A/B/D/E).

## Allowed paths

- `docs/harness-reliability.md` (Beat 2.3 section only)
- `scripts/demo/disk-watchdog.sh` (new, optional — fault-inject via tiny threshold on a scratch `--home`, never a real disk)
- `internal/queue/disk_watchdog_test.go`, `features/*.feature` + step files (tests only)
- `tasks/sprints/S03-*/**`

## Explicitly excluded

- Prod changes to `internal/queue/disk_watchdog.go` — gap found → split to a follow-up ticket (S02 PR-C pattern)
- Unrelated queue refactors

## Done when

- [ ] Low-disk condition produces a durable event or board task per Phase 2.3 pass criteria
- [ ] Demo runs offline with fault injection (documented steps, no real disk fill)
- [ ] Linked from `docs/harness-reliability.md`
- [ ] Diff within budget

## Notes

Watchdog already exists in-tree (`internal/queue/disk_watchdog.go`); this beat is prove-and-document, not build-from-scratch.
