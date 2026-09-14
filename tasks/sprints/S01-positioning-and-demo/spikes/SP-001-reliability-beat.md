# SP-001: Pick first harness reliability beat

| Field | Value |
| --- | --- |
| Type | spike |
| Status | done |
| Priority | P1 |
| Sprint | S01-positioning-and-demo |
| Time box | 0.5 day |
| Links | [product-plan Phase 2](../../../../docs/product-plan.md), [demo.md](../../../../docs/demo.md), [T-006](../../../backlog/tasks/T-006-harness-reliability-doc.md) |

## Question

Which single reliability beat should be the first scripted demo after the 10-minute path: restart mid-task, or permission → HUMAN?

## Decision

**Pick: restart mid-task** as the first S02 / US-003 reliability beat.

| Candidate | Verdict |
| --- | --- |
| **Restart mid-task** | **GO for S02** — unique to a durable board harness; proves claim/heartbeat/ghost reconcile, not just “LLM died.” |
| Permission → HUMAN | Defer — real, but fiddly to script; overlaps the demo’s already-proven HUMAN story less cleanly than restart. |
| Dead connector → HUMAN | **Already the climax of [`docs/demo.md`](../../../../docs/demo.md)** (SP-003). Do not spend S02 re-proving it; link demo.md instead. |

### Why restart first

1. Product plan Phase 2 leads with “restart mid-task” as harness quality, not coding quality.
2. SP-003 / T-004 already own connector → `Manual review required: AI providers unavailable`.
3. Restart shows **SQLite lifecycle survives process death** — the “house the loop lives in” pitch.

### Rough command list (for T-006 / `docs/harness-reliability.md`)

1. Run demo steps 0–4 so a task is `RUNNING` (or about to be claimed).
2. `kill -KILL` the `agentd start` process ( unclean; no graceful drain ).
3. `./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup` again (same home as the initial run).
4. Expect: no permanent stuck `RUNNING` without a live worker — ghost/stale reconcile returns work to `READY`/`QUEUED` or fails to a clear board state; task progresses or shows an explicit handoff.
5. Capture before/after `GET …/tasks` + `system/status`.

Permission encore (later beat): force a `sudo` (or equivalent) sandbox violation and show HUMAN / manual-action child — optional after restart doc lands.

## Output

- [x] Decision written here
- [x] Go/no-go: **restart mid-task = S02 work**; permission deferred; connector HUMAN = demo.md
- [x] Rough command list above
- [x] Stub [`docs/harness-reliability.md`](../../../../docs/harness-reliability.md) pointed at T-006

## Out of scope

Building the full reliability pack (US-003).

## Notes

Completed 2026-09-14 after T-004 landed.
