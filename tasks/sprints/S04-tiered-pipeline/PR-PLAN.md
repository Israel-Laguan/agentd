# S04 PR plan — file/LOC budgets

**Rule (per operator):** each PR ≈ **≤25 files** and/or **<1000 lines** changed (`git diff --stat` before open). Prefer smaller. No bundling runtime changes + cost harness + escalation.

Derived from [S03 retro](../S03-tiered-foundation/retro/RETRO.md): M1/M2 landed clean; S04 is the hard part — DAG splitting, worker modes, escalation, cost measurement.

## PR map (order)

| PR | Branch suggestion | Tickets | Est. files | Est. LOC | Allowed paths |
| --- | --- | --- | --- | --- | --- |
| **PR-A** | `feat/s04-tiered-m3-splitter` | T-015 | ≤15 | <600 | `internal/queue/worker/splitter*.go` + `*_test.go`, `internal/config/tiered.go` (step kind profiles), `internal/models/task.go` (DAG relation), `docs/tiered-execution.md` (M3 section), `tasks/sprints/S04-*/**` |
| **PR-B** | `feat/s04-tiered-m3-modes` | T-016 | ≤20 | <800 | `internal/queue/worker/worker_*.go` (step-kind dispatch), `internal/queue/worker/agentic/` (mode switches), `internal/queue/worker/contextpack*.go`, matching `*_test.go`, `tasks/sprints/S04-*/**`. **No** prod changes to existing legacy/agentic paths |
| **PR-C** | `feat/s04-tiered-m4-escalation` | T-017 | ≤15 | <600 | `internal/queue/worker/escalation*.go` + `*_test.go`, `internal/queue/worker/handoff*.go` (NEEDS_CONTEXT), `internal/models/enums.go` (new states), matching `*_test.go`, `docs/tiered-execution.md` (M4 section), `tasks/sprints/S04-*/**` |
| **PR-D** | `docs/s04-cost-harness` | T-018 | ≤10 | <400 | `scripts/demo/tiered-harness.sh` (new), `docs/tiered-execution.md` (M5 section), `internal/queue/worker/cost_harness_test.go`, `tasks/sprints/S04-*/**`. **No** prod change — gap found → split to follow-up ticket |
| **PR-E** | `chore/s04-t001-about` | T-001 (carry) | 0–2 | <50 | No code — `gh repo edit` + note/screenshot in `tasks/.../T-001-*.md` only. Separate from code PRs |

**Out of S04 PRs:** MCP board export (US-005, Phase 3), SWE-bench, foundational baseline contract changes.

## How to enforce

Before `gh pr create`:

```sh
git diff --stat main...HEAD
# files changed ≤ 25, insertions+deletions ideally < 1000
```

If over budget: split tests vs prod, or runtime vs docs. Never "just one more file" from another package. Escalation PR that exposes a prod gap stops at tests+docs; the fix gets its own ticket (S02 PR-C pattern).

## Ticket ↔ PR

| Ticket | PR |
| --- | --- |
| T-015 | PR-A |
| T-016 | PR-B |
| T-017 | PR-C |
| T-018 | PR-D |
| T-001 | PR-E |
