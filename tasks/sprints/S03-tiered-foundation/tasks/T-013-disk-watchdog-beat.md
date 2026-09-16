# T-013: Beat 2.3 — disk/resource watchdog demo (PR-C)

| Field | Value |
| --- | --- |
| Type | task |
| Status | done |
| Priority | P1 |
| Sprint | S03-tiered-foundation |
| Parent | US-003 (close Phase 2) |
| Estimate | M |
| PR | **PR-C** — ≤10 files / <600 LOC |
| Links | [product-plan Phase 2.3](../../../../docs/product-plan.md), [harness-reliability.md](../../../../docs/harness-reliability.md) |

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

- [x] Low-disk condition produces a durable event or board task per Phase 2.3 pass criteria — `internal/queue/disk_watchdog.go` (`checkDiskSpace`) creates a de-duplicated `_system` task assigned to `HUMAN` and emits `DISK_SPACE_CRITICAL`; covered by `internal/queue/disk_watchdog_test.go` (`TestDiskWatchdogCreatesHumanTask`, `TestDiskWatchdogDeduplicatesOpenTask`) and `internal/queue/features/disk_watchdog.feature`
- [x] Demo runs offline with fault injection (documented steps, no real disk fill) — `scripts/demo/disk-watchdog.sh` derives a threshold above observed free space on a scratch `--home`, so it triggers without touching a real disk
- [x] Linked from `docs/harness-reliability.md` — Beat 2.3 section + helper-script list
- [x] Diff within budget — docs + demo + tests only; **no prod change to `internal/queue/disk_watchdog.go`** (S02 PR-C pattern)

## Notes

Watchdog already exists in-tree (`internal/queue/disk_watchdog.go`); this beat is prove-and-document, not build-from-scratch.

No production gap found — the existing watchdog + dedup path satisfied Phase 2.3 as-is, so the "gap found → split to follow-up ticket" branch was not needed.

Shipped under **PR-C** (`docs/s03-disk-watchdog-beat`).
