# Harness reliability demos

Prove the **house**, not the coding model. Positioning: [why-agentd.md](why-agentd.md), plan Phase 2: [product-plan.md](product-plan.md).

Governance demo (approve → board → connector HUMAN) lives in [demo.md](demo.md) — do that **first**. Do not re-prove dead-`base_url` HUMAN here.

Helper scripts: [`restart-mid-task.sh`](../scripts/demo/restart-mid-task.sh) (Beat 1), [`provider-fallback.sh`](../scripts/demo/provider-fallback.sh) (Beat 2), [`disk-watchdog.sh`](../scripts/demo/disk-watchdog.sh) (Beat 2.3), [`memory-recall.sh`](../scripts/demo/memory-recall.sh) (Beat 2.4).

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

## Beat 2.3 (S03) — Disk / resource watchdog

**Tickets:** [T-013](../tasks/sprints/S03-tiered-foundation/tasks/T-013-disk-watchdog-beat.md).

Prove product-plan Phase 2.3: a disk/resource crunch surfaces a durable event or board task — no silent death. The watchdog implementation already exists (`internal/queue/disk_watchdog.go`); this beat packages it as a runnable demo with fault injection.

### Beat 2.3 — Pass criteria

1. When free disk space falls below `disk.free_threshold_percent`, the watchdog creates a `_system` HUMAN task titled **"Disk space critical. Please run cleanup or expand storage."**
2. A `DISK_SPACE_CRITICAL` SSE event is emitted with path, free percent, and threshold.
3. Deduplication: repeated low-disk checks do **not** create additional tasks while one is already open.
4. Above threshold: no task, no event — the watchdog is silent.

Mechanism: `Daemon.checkDiskSpace` → `safety.DiskFreePercent` → `EnsureProjectTask` (HUMAN assignee) → `sink.Emit(DISK_SPACE_CRITICAL)`. Tests: `internal/queue/disk_watchdog_test.go`. Feature: `internal/queue/features/disk_watchdog.feature`.

### Beat 2.3 — Prerequisites

- `make build` → `./bin/agentd`
- Throwaway home: `export AGENTD_HOME=/tmp/agentd-disk-demo`
- No billable keys required — the demo uses fault injection via a derived threshold (observed free +5%) plus a `scratch/` marker dir, never a real disk fill.

### Beat 2.3 — Operator sequence

```sh
export AGENTD_HOME=/tmp/agentd-disk-demo
export API_ADDR=127.0.0.1:18785

# A) prepare: start daemon; threshold is derived from observed free space (+5%, min 1% above current)
#    so free_percent < threshold is guaranteed without a real disk fill; scratch dir created at $AGENTD_HOME/scratch
./scripts/demo/disk-watchdog.sh prepare

# B) inject fault: (re)create scratch dir $AGENTD_HOME/scratch and ensure config free_threshold_percent
#    is set to derived value (observed free +5%, capped at 99) — no filesystem fill needed
./scripts/demo/disk-watchdog.sh inject-fault

# C) probe: verify HUMAN task, SSE event, and deduplication (exactly 1 task, assignee=HUMAN)
./scripts/demo/disk-watchdog.sh probe
# expect: _system project with HUMAN task "Disk space critical..." + DISK_SPACE_CRITICAL in SSE/daemon.log

# D) cleanup
./scripts/demo/disk-watchdog.sh stop
```

### Beat 2.3 — What "good" looks like

| Condition | Expected |
| --- | --- |
| Free space **below** threshold | `_system` HUMAN task + `DISK_SPACE_CRITICAL` event |
| Free space **above** threshold | No task, no event |
| Repeated low-disk check | Deduplicated — single task, single event |
| Daemon restart after cleanup | Watchdog resumes; no stale RUNNING tasks |

### Beat 2.3 — Test coverage

- `TestDiskWatchdogCreatesHumanTask` — creates HUMAN task + DISK_SPACE_CRITICAL event
- `TestDiskWatchdogDeduplicatesOpenTask` — second check does not duplicate
- `TestDiskWatchdogNoAlertAboveThreshold` — above threshold = no alert
- `TestDiskWatchdogDelayUsesEveryWhenConfigured` — interval config respected
- `disk_watchdog.feature` — 3 scenarios matching the above

### Out of scope for Beat 2.3

- Prod changes to `disk_watchdog.go` — gap found → split to follow-up ticket (S02 PR-C pattern)
- Restart mid-task (Beat 1)
- Provider fallback (Beat 2)
- Tiered execution

## Beat 2.4 (S03) — Memory recall on repeat failure

**Tickets:** [T-014](../tasks/sprints/S03-tiered-foundation/tasks/T-014-memory-recall-beat.md).

Prove product-plan Phase 2.4: on a repeated failure class, Librarian/FTS surfaces a prior `{symptom, solution}` instead of re-burning tokens. The recall mechanism already exists (`internal/memory/recall.go`); this beat packages it as a runnable demo with seeded fixtures.

### Beat 2.4 — Pass criteria

1. A seeded `{symptom, solution}` USER_PREFERENCE memory is retrievable via `Retriever.Recall` (FTS by intent) when a matching intent arrives.
2. `FormatPreferences` renders the recalled pair into a system prompt block labeled **"USER PREFERENCES"** (lesson-scope `FormatLessons` is exercised in `recall_test.go` with `GLOBAL`/`TASK_CURATION` scopes).
3. Namespace isolation holds: project-scoped memories do not leak across projects.
4. Recall timeout is respected: a slow store returns empty results, not a hang.

Mechanism: `Retriever.Recall` → `Store.RecallMemories` (FTS by intent, scoped to GLOBAL + project + user prefs) → `FormatLessons` / `FormatPreferences`. The demo seeds via `POST /api/v1/preferences` (USER_PREFERENCE scope; `Symptom="preference"`, `Solution="Symptom: … → Solution: …"`), probes via `GET /api/v1/system/status` asserting `total_memories>0` and `preferences_count>0`, plus a retrieval-and-formatting assertion that the seeded symptom/solution appears in the simulated `FormatPreferences` output (see `scripts/demo/memory-recall.sh:cmd_probe`). Tests: `internal/memory/recall_test.go`, features: `recall_namespace.feature`, `recall_timeout.feature`.

### Beat 2.4 — Prerequisites

- `make build` → `./bin/agentd`
- Throwaway home: `export AGENTD_HOME=/tmp/agentd-recall-demo`
- No billable keys required — the demo uses a mock LLM and seeds memories via the preferences API.

### Beat 2.4 — Operator sequence

```sh
export AGENTD_HOME=/tmp/agentd-recall-demo
export API_ADDR=127.0.0.1:18795

# A) start daemon with a mock provider
./scripts/demo/memory-recall.sh prepare

# B) seed a {symptom, solution} pair via the preferences API (JSON-encoded via python3; handles quotes/backslashes)
./scripts/demo/memory-recall.sh seed "EOFError when parsing JSON" "Add try/except around json.loads with fallback to raw text"
# expect: HTTP 201 saved; payload built with json.dumps so special chars are safe

# C) probe: verify daemon health and retrieval + formatting assertion
./scripts/demo/memory-recall.sh probe
# expect: runtime memory section present + FormatPreferences contains symptom/solution

# D) cleanup
./scripts/demo/memory-recall.sh stop
```

### Beat 2.4 — What "good" looks like

| Condition | Expected |
| --- | --- |
| Seeded `{symptom, solution}` (USER_PREFERENCE) | Recall returns the pair; `FormatPreferences` renders `Symptom: … → Solution: …` |
| Different project scope | Project memory does not leak to other projects |
| Slow DB (timeout) | Recall returns empty within timeout; no hang |
| No memories seeded | Recall returns empty; chat proceeds without context |

### Beat 2.4 — Test coverage

- `TestRetriever_NamespaceIsolation` — GLOBAL + project scope isolation
- `TestRetriever_TimeoutFallback` — slow store returns within timeout
- `TestRetriever_NilRetriever` — nil retriever returns nil
- `TestFormatLessons` — renders symptom/solution pairs
- `recall_namespace.feature` — 2 scenarios (global/project recall, user prefs)
- `recall_timeout.feature` — 1 scenario (slow DB fallback)

### Out of scope for Beat 2.4

- Prod changes to recall/librarian paths — gap found → split to follow-up ticket (S02 PR-C pattern)
- Dream consolidation tuning, embedding-model swaps
- Restart mid-task (Beat 1)
- Provider fallback (Beat 2)
- Tiered execution

## Later beats

| Beat | Intent | Ticket |
| --- | --- | --- |
| Provider fallback (**Beat 2**, above) | Cascade or breaker/HUMAN | [T-010a](../tasks/sprints/S02-harness-reliability/tasks/T-010a-fallback-beat-docs.md) |
| Disk / watchdog (**Beat 2.3**, above) | Durable watchdog signal | [T-013](../tasks/sprints/S03-tiered-foundation/tasks/T-013-disk-watchdog-beat.md) |
| Memory recall | Librarian/FTS on repeat failure | [T-014](../tasks/sprints/S03-tiered-foundation/tasks/T-014-memory-recall-beat.md) |
| Permission → HUMAN | Sandbox violation | later encore |

## Related

- Connector inject HUMAN: [demo.md](demo.md)
- SP-003 run log: [SP-003](../tasks/sprints/S01-positioning-and-demo/spikes/SP-003-demo-path-dry-run.md)
- SP-004 pre-flight: [SP-004](../tasks/sprints/S02-harness-reliability/spikes/SP-004-env-preflight.md)
- S02 PR budgets: [PR-PLAN.md](../tasks/sprints/S02-harness-reliability/PR-PLAN.md)
