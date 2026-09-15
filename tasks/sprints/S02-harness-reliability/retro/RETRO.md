# Retro: S02 — Harness reliability pack

| Field | Value |
| --- | --- |
| Sprint | S02-harness-reliability |
| Date | 2026-10-13 |

## Went well

- Beat 1 shipped complete (docs + script + tests): restart reconcile coverage proves unclean kills reset dead-PID tasks to `READY` and preserve live `RUNNING` (`tasks/.../T-006b-restart-reconcile-tests.md:1`); T-006b found **no prod gap**, so T-006c correctly stayed `backlog` — the reconcile logic was already right.
- Beat 2 shipped complete: secondary-provider fallback, `ErrLLMUnreachable` classification, breaker-open threshold all covered (`tasks/.../T-010b-cascade-breaker-tests.md:1`).
- S01 retro actions: README First Run now leads with LiteLLM/Poolside/mock instead of stale Gemini-only (`README.md:41`); SP-004 pre-flight done; **unresolved carry: the `gemini-2.5-flash` default remains in `internal/config/gateway.go`, `config.reference.yaml`, and `docs/config-reference.md`, yet SP-003 documents it as returning 404; T-012 required a valid id or empty model + order-only cascade, so the stale default is still unshipped.**
- PR budgets held: PR-F landed 3 in-scope files (~48 net lines, budget ≤5/<250); PR-G landed 10 code files, 34 changed lines (budget ≤15/<400).

## Went poorly

- Demo scripts needed ~7 iterative fix/refactor commits (`bab8eec` → `17a6cb4`: process matching, validation regex, error-body handling) — should have been one hardened pass, not converge-by-commit.
- Pre-existing uncommitted script changes got bundled into the PR-F commit (`17a6cb4`: `scripts/demo/*` rode along with the README edit), slightly breaching PR-F's allowlist.
- Status hygiene lagged the work: T-006/T-011/T-012 flips happened after the commits, not with them; T-010 wrapper and US-003 still read `ready` despite all children done.
- T-001 carry (PR-H, `gh` auth) still unapplied at close — same auth gap S01 flagged.

## Surprises

- Restart reconcile needed no production fix at all — the "fix if tests expose a gap" conditional (PR-C) resolved to a skip, which the PR-PLAN's conditional design handled cleanly.
- Renaming one README heading fanned out to 3 files (`README.md`, `docs/config-reference.md`, `docs/init-startup.md`) — anchor-link coupling is a small but real docs tax.
- Shell-metacharacter validation needed three regex iterations (quotes/backslashes) before stabilizing — edge cases in `BIN` validation were underestimated.

## Actions (assign + due)

| Action | Owner | Due |
| --- | --- | --- |
| Flip T-010 wrapper + US-003 to `done` (children all done) | sprint owner | S02 close |
| Apply T-001 GitHub About/topics (PR-H) once `gh` auth works | repo admin (operator) | S03 planning |
| Weekly 30–45m refinement: groom tiered `US-004`/`T-007`/`T-008` for S03 (per `README.md:43`) | facilitator TBD | before S03 start |
| Demo-script rule: harden + shellcheck in one pass before first commit, no converge-by-commit | docs/scripts owner | S03 |

## Carry into next sprint

- **S03 = tiered execution** (`US-004` / `T-007` / `T-008`) per sprint README grooming note — S02 proved the reliability floor (restart + fallback degrade to board state); S03 builds scheduling policy on top.
- T-006c stays `backlog` permanently unless a future beat re-proves a reconcile gap — do not carry as active work.
- Process carry: flip task statuses in the same commit as the work, and keep PR allowlists strict (no unrelated files riding along).
