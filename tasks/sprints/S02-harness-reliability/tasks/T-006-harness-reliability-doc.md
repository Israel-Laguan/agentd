# T-006: docs/harness-reliability.md + first beat

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P0 |
| Sprint | S02-harness-reliability |
| Parent | US-003 |
| Estimate | L |
| Links | [SP-001](../../S01-positioning-and-demo/spikes/SP-001-reliability-beat.md), [harness-reliability](../../../../docs/harness-reliability.md) |

## Goal

Document and script the first reliability beat chosen in SP-001.

## Done when

- [ ] `docs/harness-reliability.md` Beat 1 (restart mid-task) has pass criteria — no silent stuck `RUNNING`; same-`--home` restart asserts at least one allowed outcome: work progresses, task returns to `READY`/`QUEUED`, or work reaches an explicit handoff
- [ ] Commands or fixtures listed: `kill -KILL <agentd-pid>` (unclean) → `./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup` with same `--home` → `GET /api/v1/projects/{id}/tasks` + `GET /api/v1/system/status` before/after

## Notes

SP-001 decision: **restart mid-task** is Beat 1 (`../../S01-positioning-and-demo/spikes/SP-001-reliability-beat.md`). Stub: `../../../../docs/harness-reliability.md`. Connector HUMAN stays in `../../../../docs/demo.md` — do not re-prove.
Rough command list is in `spikes/SP-001-reliability-beat.md:33`. Gate with SP-004 env pre-flight.
