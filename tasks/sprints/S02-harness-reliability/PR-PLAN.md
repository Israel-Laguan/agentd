# S02 PR plan — file/LOC budgets

**Rule (per operator):** each PR ≈ **≤25 files** and/or **&lt;1000 lines** changed (`git diff --stat` before open). Prefer smaller. No bundling docs+large code+config renames.

Derived from [S01 retro](../S01-positioning-and-demo/retro/RETRO.md): pre-flight gap, stale Gemini default, restart Beat 1, fallback Beat 2, keep tiered/MCP out.

## PR map (order)

| PR | Branch suggestion | Tickets | Est. files | Est. LOC | Allowed paths |
| --- | --- | --- | --- | --- | --- |
| **PR-A** | `docs/s02-reliability-restart-doc` | T-006a | ≤8 | &lt;400 | `docs/harness-reliability.md`, `docs/demo.md` (link only), `README.md` (one link), `tasks/sprints/S02-*/**`, optional `scripts/demo/restart-mid-task.sh` |
| **PR-B** | `test/s02-restart-reconcile` | T-006b | ≤15 | &lt;800 | `internal/queue/**` tests + features only (`*_test.go`, `features/*.feature`, step files). **No** production `.go` unless a one-line fix is required to make an existing assertion true — then split that fix to PR-C |
| **PR-C** | `fix/s02-restart-reconcile` | T-006c (only if B finds a gap) | ≤10 | &lt;600 | `internal/queue/heartbeat_reconcile.go`, `internal/kanban/tasks_repo.go` (ghost/stale claim paths), matching `*_test.go` only |
| **PR-D** | `docs/s02-provider-fallback` | T-010a | ≤6 | &lt;350 | `docs/harness-reliability.md` Beat 2, optional `scripts/demo/provider-fallback.sh`, `tasks/**` |
| **PR-E** | `test/s02-provider-cascade` | T-010b | ≤12 | &lt;700 | `internal/gateway/**` and/or `internal/queue/safety/**` **tests** for cascade + breaker; fixtures under those packages. Avoid drive-by refactors |
| **PR-F** | `docs/s02-litellm-first-run` | T-011 | ≤5 | &lt;250 | `README.md` (First Run section), `docs/demo.md` (already LiteLLM-first — align only), `config.reference.yaml` comment/example for LiteLLM — **not** mass test renames |
| **PR-G** | `chore/s02-gemini-default` | T-012 | ≤15 | &lt;400 | Default string swap `gemini-2.5-flash` → chosen alias **or** document “unset / use LiteLLM” in `internal/config/gateway.go` + `config.reference.yaml` + `docs/config-reference.md` + tests that assert the default. Keep to the ~11 main-tree hits; **exclude** `.kilo/worktrees/**` |
| **PR-H** | `chore/s02-t001-about` | T-001 (carry) | 0–2 | &lt;50 | No code — `gh repo edit` + note/screenshot in `tasks/.../T-001-*.md` only. Separate from code PRs |

**Out of S02 PRs:** `US-004` / `T-007` / `T-008` (tiered), `US-005` (MCP), disk-watchdog beat, SWE-bench.

## How to enforce

Before `gh pr create`:

```sh
git diff --stat main...HEAD
# files changed ≤ 25, insertions+deletions ideally < 1000
```

If over budget: split tests vs prod, or docs vs code. Never “just one more file” from another package.

## Ticket ↔ PR

| Ticket | PR |
| --- | --- |
| SP-004 | no PR (local checklist); blocks starting gh-gated H |
| T-006a | PR-A |
| T-006b | PR-B |
| T-006c | PR-C (conditional) |
| T-010a | PR-D |
| T-010b | PR-E |
| T-011 | PR-F |
| T-012 | PR-G |
| T-001 | PR-H |
