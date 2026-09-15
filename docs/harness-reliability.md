# Harness reliability demos

Prove the **house**, not the coding model. Positioning: [why-agentd.md](why-agentd.md), plan Phase 2: [product-plan.md](product-plan.md).

Governance demo (approve → board → connector HUMAN) lives in [demo.md](demo.md) — do that **first**. Do not re-prove dead-`base_url` HUMAN here.

Helper scripts: [`restart-mid-task.sh`](../scripts/demo/restart-mid-task.sh) (Beat 1), [`provider-fallback.sh`](../scripts/demo/provider-fallback.sh) (Beat 2).

## Beat 1 (S02) — Restart mid-task

**Chosen in** [SP-001](../tasks/sprints/S01-positioning-and-demo/spikes/SP-001-reliability-beat.md).  
**Tickets:** [T-006a](../tasks/sprints/S02-harness-reliability/tasks/T-006a-restart-beat-docs.md) (this doc/script), [T-006b](../tasks/sprints/S02-harness-reliability/tasks/T-006b-restart-reconcile-tests.md) (tests).

### Beat 1 — Pass criteria

After an **unclean** kill of `agentd` while work is in flight (or claimed), then `start` again with the **same** `--home` / `AGENTD_HOME`:

1. The board must **not** leave a task stuck in `RUNNING` with no live worker forever.
2. At least one allowed outcome must hold for the in-flight task:
   - returns to `READY` / `QUEUED` and can be claimed again, **or**
   - progresses toward `COMPLETED`, **or**
   - reaches an **explicit** failure / handoff board state (not a silent hang).
3. SQLite home remains usable (daemon starts; `GET /api/v1/system/status` succeeds).

Ghost/stale reconcile (`ReconcileGhostTasks` / `ReconcileStaleTasks` + heartbeat loop) is the mechanism under test — see `internal/queue/features/ghost_reconciliation.feature` and `heartbeat_reconciliation.feature`.

### Beat 1 — Prerequisites

- `make build` → `./bin/agentd`
- Throwaway home recommended: `export AGENTD_HOME=/tmp/agentd-restart-demo`
- A configured provider so `start` comes up (LiteLLM/Poolside/mock, **or** a dummy openai-compatible slot — see script `prepare`). Empty keys → process exits with `no LLM providers available` (SP-004).

### Beat 1 — Operator sequence

```sh
export AGENTD_HOME=/tmp/agentd-restart-demo
export API_ADDR=127.0.0.1:18765   # must match config api.address

# A) home + daemon (dummy upstream is enough to listen)
./scripts/demo/restart-mid-task.sh prepare

# B) put a task on the board (pick one):
#    - follow docs/demo.md ask→approve→workspace/ready with a working connector, or
#    - materialize a DraftPlan via POST /api/v1/projects/materialize, seed workspace, POST …/workspace/ready
# Optional: leave a worker RUNNING by using a slow/mock LLM.

PROJECT_ID=…   # from GET /api/v1/projects

# C) before
./scripts/demo/restart-mid-task.sh status
curl -sS "http://${API_ADDR}/api/v1/projects/$PROJECT_ID/tasks"

# D) unclean stop + same-home start
./scripts/demo/restart-mid-task.sh cycle
# equivalent:
#   kill -KILL "$(pgrep -f "./bin/agentd --home $AGENTD_HOME start")"
#   ./bin/agentd --home "$AGENTD_HOME" start --skip-llm-warmup

# E) after — assert pass criteria
curl -sS "http://${API_ADDR}/api/v1/projects/$PROJECT_ID/tasks"
curl -sS "http://${API_ADDR}/api/v1/system/status"
```

### What “good” looks like

| Before kill | After restart (examples) |
| --- | --- |
| `RUNNING` | `READY` / `QUEUED` / `COMPLETED` / `BLOCKED`+HUMAN / `FAILED*` — **not** eternal `RUNNING` |
| Daemon PID gone | New PID; API 200 on `/api/v1/system/status` |

### Out of scope for Beat 1

- Provider cascade / breaker HUMAN (Beat 2 / [demo.md](demo.md))
- Permission/`sudo` HUMAN
- Tiered execution

## Beat 2 (S02) — Provider fallback / breaker

**Tickets:** [T-010a](../tasks/sprints/S02-harness-reliability/tasks/T-010a-fallback-beat-docs.md) (this doc/script), [T-010b](../tasks/sprints/S02-harness-reliability/tasks/T-010b-cascade-breaker-tests.md) (tests).

Distinct from [demo.md](demo.md)’s connector-HUMAN **governance** loop. Beat 2 proves the **house**: cascade to a healthy secondary, or open the breaker / hand off when nothing answers — without a billable cloud dependency (mock HTTP, LiteLLM, or dead `127.0.0.1:1` slots).

### Beat 2 — Pass criteria

**Scenario A — cascade success**

1. Gateway `order` has ≥2 openai-compatible entries (or any adapters).
2. Primary is unreachable (dead `base_url` / refusing mock).
3. Secondary answers successfully.
4. Request completes with `ProviderUsed` = secondary; board work is not stuck eternal `RUNNING` solely because primary died.

**Scenario B — breaker / HUMAN (exhaustion)**

1. Only one provider configured **or** every entry in `order` is unreachable.
2. Failures classify as breaker failures (`ErrLLMUnreachable` / quota).
3. After the configured failure threshold, breaker is **OPEN**.
4. With outage handoff enabled long enough, a `_system` HUMAN diagnostic appears (title contains “System Offline” / AI API) — **breaker state**, not the approve→HUMAN path in `demo.md`.

Mechanism pointers: gateway cascade (`internal/gateway/features/cascading_fallback.feature`), breaker (`internal/queue/features/circuit_breaker.feature`), handoff (`outage_handoff.feature`).

### Beat 2 — Prerequisites

- `make build` → `./bin/agentd`
- Throwaway home: `export AGENTD_HOME=/tmp/agentd-fallback-demo`
- No billable keys required — script uses local mock HTTP + dead ports.

### Beat 2 — Operator sequence

```sh
export AGENTD_HOME=/tmp/agentd-fallback-demo
export API_ADDR=127.0.0.1:18776

# A) two-slot config: dead primary + live mock secondary
./scripts/demo/provider-fallback.sh prepare-cascade
./scripts/demo/provider-fallback.sh probe-cascade
# expect: HTTP 200 from mock secondary path / ProviderUsed secondary in logs

# B) single dead slot → exhaustion
./scripts/demo/provider-fallback.sh prepare-breaker
./scripts/demo/provider-fallback.sh probe-breaker
# expect: ErrLLMUnreachable-class failure; after N failures breaker OPEN
# optional: leave daemon running until outage handoff creates HUMAN under _system

./scripts/demo/provider-fallback.sh status
./scripts/demo/provider-fallback.sh stop
```

### Beat 2 — What "good" looks like

| Setup | Expected |
| --- | --- |
| Dead primary + healthy secondary | Success via secondary (`ProviderUsed` ≠ primary) |
| All dead / single dead | Cascade exhausts → `ErrLLMUnreachable`; breaker OPEN after threshold; eventual `_system` HUMAN if handoff threshold met |
| Never | Silent hang forever on primary with no board signal |

### Out of scope for Beat 2

- Restart mid-task (Beat 1)
- Permission/`sudo` HUMAN
- Re-proving ask→approve→connector HUMAN (`demo.md`)
- Tiered execution

## Later beats

| Beat | Intent | S02 ticket |
| --- | --- | --- |
| Provider fallback (**Beat 2**, above) | Cascade or breaker/HUMAN | [T-010a](../tasks/sprints/S02-harness-reliability/tasks/T-010a-fallback-beat-docs.md) |
| Disk / watchdog | Durable watchdog signal | later |
| Memory recall | Librarian/FTS on repeat failure | later |
| Permission → HUMAN | Sandbox violation | later encore |

## Related

- Connector inject HUMAN: [demo.md](demo.md)
- SP-003 run log: [SP-003](../tasks/sprints/S01-positioning-and-demo/spikes/SP-003-demo-path-dry-run.md)
- SP-004 pre-flight: [SP-004](../tasks/sprints/S02-harness-reliability/spikes/SP-004-env-preflight.md)
- S02 PR budgets: [PR-PLAN.md](../tasks/sprints/S02-harness-reliability/PR-PLAN.md)
