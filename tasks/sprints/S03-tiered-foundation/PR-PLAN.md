# S03 PR plan — file/LOC budgets

**Rule (per operator):** each PR ≈ **≤25 files** and/or **<1000 lines** changed (`git diff --stat` before open). Prefer smaller. No bundling tiered runtime + beat docs + prod fixes.

Derived from [S02 retro](../S02-harness-reliability/retro/RETRO.md): one-pass script hardening, strict allowlists, statuses flipped with the work.

## PR map (order)

| PR | Branch suggestion | Tickets | Est. files | Est. LOC | Allowed paths |
| --- | --- | --- | --- | --- | --- |
| **PR-A** | `feat/s03-tiered-m1-gate` | T-007 | ≤10 | <500 | `internal/config/**` (tiered keys), `internal/queue/**` or `internal/frontdesk/**` (gate plumbing only), matching `*_test.go`, `config.reference.yaml` (tiered keys only), `tasks/sprints/S03-*/**` |
| **PR-B** | `feat/s03-tiered-m2-contextpack` | T-008 | ≤15 | <800 | `internal/config/**` (ContextPack schema keys), `internal/queue/contextpack*.go`, `internal/frontdesk/contextpack*.go` + matching `*_test.go`, `config.reference.yaml` (ContextPack keys only), `tasks/sprints/S03-*/**`. Size-limit enforcement only. No splitter, no allowlists, no escalation |
| **PR-C** | `docs/s03-disk-watchdog-beat` | T-013 | ≤10 | <600 | `docs/harness-reliability.md` (Beat 2.3), optional `scripts/demo/disk-watchdog.sh`, `internal/queue/disk_watchdog_test.go` + features only, `tasks/sprints/S03-*/**`. **No** prod change to `disk_watchdog.go` — gap found → split to follow-up ticket |
| **PR-D** | `docs/s03-memory-recall-beat` | T-014 | ≤10 | <600 | `docs/harness-reliability.md` (Beat 2.4), `internal/memory/**` recall/librarian `*_test.go` + features only, optional `scripts/demo/memory-recall.sh`, `tasks/sprints/S03-*/**`. **No** prod change — gap found → split to follow-up ticket |
| **PR-E** | `chore/s03-t001-about` | T-001 (carry) | 0–2 | <50 | No code — `gh repo edit` + note/screenshot in `tasks/.../T-001-*.md` only. Separate from code PRs |

**Out of S03 PRs:** tiered M3–M5 (splitter, allowlists, escalation, cost harness → S04), `US-005` (MCP), SWE-bench.

## How to enforce

Before `gh pr create`:

```sh
git diff --stat main...HEAD
# files changed ≤ 25, insertions+deletions ideally < 1000
```

If over budget: split tests vs prod, or runtime vs docs. Never “just one more file” from another package. Beat PRs that expose a prod gap stop at tests+docs; the fix gets its own ticket (S02 PR-C pattern).

## Ticket ↔ PR

| Ticket | PR |
| --- | --- |
| T-007 | PR-A |
| T-008 | PR-B |
| T-013 | PR-C |
| T-014 | PR-D |
| T-001 | PR-E |
