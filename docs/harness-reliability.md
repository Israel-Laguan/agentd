# Harness reliability demos

Prove the **house**, not the coding model. Positioning: [why-agentd.md](why-agentd.md), plan Phase 2: [product-plan.md](product-plan.md).

Governance demo (approve → board → connector HUMAN) lives in [demo.md](demo.md) — do that first.

## Beat 1 (S02) — Restart mid-task

**Status:** chosen in [SP-001](../tasks/sprints/S01-positioning-and-demo/spikes/SP-001-reliability-beat.md); script/doc expansion is [T-006](../tasks/backlog/tasks/T-006-harness-reliability-doc.md).

**Pass criteria:** after killing `agentd` during an in-flight task and starting again with the same home, the board has no silent stuck `RUNNING`; work resumes or fails to an explicit state.

**Sketch:**

```sh
# with AGENTD_HOME set and a READY/RUNNING task visible…
kill -KILL <agentd-pid>    # unclean stop (non-graceful)
./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup
curl -sS "http://127.0.0.1:8765/api/v1/projects/$PROJECT_ID/tasks"
curl -sS "http://127.0.0.1:8765/api/v1/system/status"
```

## Later beats

| Beat | Intent |
| --- | --- |
| Provider fallback | Kill primary; cascade continues or opens system/HUMAN task |
| Disk / watchdog | Watchdog surfaces durable event or task |
| Memory recall | Repeated failure class hits librarian/FTS lesson |
| Permission → HUMAN | Sandbox violation → human child (encore; not Beat 1) |

## Related

- Connector inject HUMAN: [demo.md](demo.md)
- SP-003 run log: [SP-003](../tasks/sprints/S01-positioning-and-demo/spikes/SP-003-demo-path-dry-run.md)
