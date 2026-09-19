# S04 PR plan — file/LOC budgets

**Rule (per operator):** each PR ≈ **≤50 files** and/or **<1000 lines** changed (`git diff --stat` before open), with a per-PR minimum floor (see table) so PRs land with substantive, complete scope rather than token-sized slices.

Derived from [S03 retro](../S03-tiered-foundation/retro/RETRO.md): M1/M2 landed clean; S04 is the hard part — DAG splitting, worker modes, escalation, cost measurement.

## PR map (order)

| PR | Branch suggestion | Tickets | Est. files | Est. LOC | Allowed paths |
| --- | --- | --- | --- | --- | --- |
| **PR-A** | `feat/s04-tiered-m3-splitter` | T-015 | ≤15 | <600 | `internal/queue/worker/splitter*.go` + `*_test.go`, `internal/config/tiered.go` (step kind profiles), `internal/models/task.go` (DAG relation), `docs/tiered-execution.md` (M3 section), `tasks/sprints/S04-*/**` |
| **PR-B** | `feat/s04-tiered-m3-modes` | T-016 | 20–50 | <800 | `internal/queue/worker/worker_*.go` (step-kind dispatch), `internal/queue/worker/agentic/` (mode switches), `internal/queue/worker/contextpack*.go`, matching `*_test.go`, `tasks/sprints/S04-*/**`. **No** prod changes to existing legacy/agentic paths |
| **PR-C** | `feat/s04-tiered-m4-escalation-harness` | T-017 + T-018 | 20–50 | <1000 | `internal/queue/worker/escalation*.go` + `*_test.go`, `internal/queue/worker/handoff*.go` (NEEDS_CONTEXT), `internal/models/enums.go` (new states), `scripts/demo/tiered-harness.sh` (new), `internal/queue/worker/cost_harness_test.go`, `docs/tiered-execution.md` (M4 + M5 sections), matching `*_test.go`, `tasks/sprints/S04-*/**`. Merged per operator decision 2026-09-19 — see note below |

**Out of S04 PRs:** MCP board export (US-005, Phase 3), SWE-bench, foundational baseline contract changes.

## How to enforce

Before `gh pr create`:

```sh
git diff --stat main...HEAD
# files changed within the PR's own range in the table above (e.g. PR-B/PR-C's 20-50), insertions+deletions within the PR's own LOC cap (e.g. PR-A <600, PR-B <800)
```

If over budget: split tests vs prod, or runtime vs docs. Never "just one more file" from another package. Escalation PR that exposes a prod gap stops at tests+docs; the fix gets its own ticket (S02 PR-C pattern).

**PR-B floor (20 files minimum):** step-kind dispatch + mode switching genuinely touches worker_*.go, agentic/, and contextpack*.go across several files plus matching tests — landing under 20 there likely means dispatch wiring was left incomplete (e.g. only one mode wired, or tests skipped for some step kinds). Verify actual completeness against T-016's "Done when" list rather than adding files just to clear the floor.

**PR-C floor (20 files minimum) — merged T-017 + T-018:** originally separate PRs; **operator decision 2026-09-19** merged them to clear the 20-file floor. This reverses the old PR-PLAN rule "Prefer smaller. No bundling runtime changes + cost harness + escalation." and README's "PRs that mix runtime changes + cost harness + escalation" rule (both now superseded for PR-C specifically). Tradeoff accepted knowingly: the escalation ladder (new states, HUMAN handoff — a real runtime behavior change) and the cost harness (T-018, explicitly no-prod-change demo script + docs) now review together despite different risk profiles. Landing under 20 files likely means either escalation's state machine or the harness's fixed task pack was left incomplete — verify against both T-017's and T-018's "Done when" lists, not by padding.

PR-A keeps its own ≤15 files/<600 LOC ceiling with no floor — its actual scope (T-015, a pure constructor) is legitimately small, and the sprint's "prefer smaller" instinct still applies wherever a floor isn't explicitly set.

## Ticket ↔ PR

| Ticket | PR |
| --- | --- |
| T-015 | PR-A |
| T-016 | PR-B |
| T-017 | PR-C |
| T-018 | PR-C |
