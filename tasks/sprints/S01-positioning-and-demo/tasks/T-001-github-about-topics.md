# T-001: GitHub About + topics

| Field | Value |
| --- | --- |
| Type | task |
| Status | in-progress |
| Priority | P0 |
| Sprint | S01-positioning-and-demo |
| Parent | US-001 |
| Estimate | S |
| Links | [product-plan 0.1–0.2](../../../../docs/product-plan.md) |

## Goal

Set repository About description to the one-liner and add discovery topics.

## Done when

- [x] About field set — `Local daemon that turns an approved plan into a durable Kanban board and runs sandboxed workers against it — model-agnostic, SQLite source of truth.` (apply via `gh repo edit` — see README)
- [x] Topics visible on repo page — `agent-harness`, `local-first`, `kanban`, `sqlite`, `multi-agent`, `golang` (apply via `gh repo edit --add-topic ...`)
- [ ] Screenshot or note in PR/commit message for traceability — attach `gh repo view --json description,repositoryTopics` output to PR (retro notes GH auth gap — pending PR evidence)

## Notes

One-liner from product plan:

> Local daemon that turns an approved plan into a durable Kanban board and runs sandboxed workers against it — model-agnostic, SQLite source of truth.

## Blocked without API auth — resolved 2026-09-14 (traceability pending 2026-09-15, status `in-progress`)

Was blocked. Retro records fix: operator applies `gh repo edit` (or web UI) with text/topics above; session missed `gh` install → new SP-004 pre-flight spike.
Local branch `docs/s01-positioning` holds T-002/T-003 until push; T-001 is `in-progress` pending PR evidence (`gh repo view --json description,repositoryTopics` screenshot — retro action due 2026-09-15, see `retro/RETRO.md`). Parent `US-001` is likewise `in-progress` conditional on that verification.
