# Harness reliability journeys

Prove the **house**, not the coding model. Positioning: [why-agentd.md](why-agentd.md), plan Phase 2: [product-plan.md](product-plan.md).

Governance demo (approve → board → connector HUMAN) lives in [demo.md](demo.md) — do that **first**. Do not re-prove dead-`base_url` HUMAN here.

These beats are proven by the e2e journey suite, not by helper scripts: each beat
below names the journey that covers it. Run everything with `make test-e2e`
(brings up the `devenv/` stack on rootless Podman and runs the suite). Journey
specs, pass criteria and gotchas: [journeys.md](testing/journeys.md).

| Beat | Journey | What it proves |
| --- | --- | --- |
| Beat 1 — Restart mid-task | **J08** (`TestJ08_UncleanKillRecovery`) | Unclean kill mid-task, restart on the same home, no stuck `RUNNING` |
| Beat 2 — Provider fallback / breaker | **J09** (`TestJ09_ProviderCascade`, `TestJ09_BreakerOpens`) | Cascade to a live secondary; breaker OPEN + HUMAN handoff when every provider is dead |
| Beat 2.3 — Disk / resource watchdog | **J10** (`TestJ10_DiskWatchdogDedup`) | One deduped HUMAN "Disk space critical" task below threshold |
| Beat 2.4 — Memory recall | **J11** (`TestJ11_PreferenceRecallOnLaterTask`) | A saved preference is recalled and shown to the agent on a later task |

## Beat 1 (S02) — Restart mid-task

**Chosen in** SP-001.
**Tickets:** T-006a (this beat), T-006b (tests).
**Journey:** J08 in [journeys.md](testing/journeys.md) — `make test-e2e`.

### Beat 1 — Pass criteria

After an **unclean** kill of `agentd` while work is in flight (or claimed), then `start` again with the **same** `--home` / `AGENTD_HOME`:

1. The board must **not** leave a task stuck in `RUNNING` with no live worker forever.
2. At least one allowed outcome must hold for the in-flight task:
   - returns to `READY` / `QUEUED` and can be claimed again, **or**
   - progresses toward `COMPLETED`, **or**
   - reaches an **explicit** failure / handoff board state (not a silent hang).
3. SQLite home remains usable (daemon starts; `GET /api/v1/system/status` succeeds).

Ghost/stale reconcile (`ReconcileGhostTasks` / `ReconcileStaleTasks` + heartbeat loop) is the mechanism under test — see `internal/queue/features/ghost_reconciliation.feature` and `heartbeat_reconciliation.feature`.

### Beat 1 — Operator sequence

```sh
make test-e2e   # runs TestJ08_UncleanKillRecovery against the devenv stack
```

The journey materializes a multi-task plan, SIGKILLs the daemon mid-task,
restarts it on the same home, and asserts: no `RUNNING` tasks after restart,
`system/status` returns 200, recovery is automatic (`BootReconcile`).

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

**Tickets:** T-010a (this beat), T-010b (tests).
**Journey:** J09 in [journeys.md](testing/journeys.md) — `make test-e2e`.

Distinct from [demo.md](demo.md)’s connector-HUMAN **governance** loop. Beat 2 proves the **house**: cascade to a healthy secondary, or open the breaker / hand off when nothing answers — without a billable cloud dependency (the journeys use dead `127.0.0.1` slots and the mock LLM).

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

### Beat 2 — Operator sequence

```sh
make test-e2e   # runs TestJ09_ProviderCascade (faults profile) and TestJ09_BreakerOpens (breaker profile)
```

J09 runs as two tests on two profiles: `faults` (dead primary + live secondary
→ cascade succeeds, breaker stays CLOSED) and `breaker` (every provider dead →
breaker OPEN, HUMAN "Manual review required: AI providers unavailable" child
created).

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

**Tickets:** T-013.
**Journey:** J10 in [journeys.md](testing/journeys.md) — `make test-e2e`.

Prove product-plan Phase 2.3: a disk/resource crunch surfaces a durable event or board task — no silent death. The watchdog implementation already exists (`internal/queue/disk_watchdog.go`); J10 proves it against a live daemon.

### Beat 2.3 — Pass criteria

1. When free disk space falls below `disk.free_threshold_percent`, the watchdog creates a `_system` HUMAN task titled **"Disk space critical. Please run cleanup or expand storage."**
2. A `DISK_SPACE_CRITICAL` SSE event is emitted with path, free percent, and threshold.
3. Deduplication: repeated low-disk checks do **not** create additional tasks while one is already open.
4. Above threshold: no task, no event — the watchdog is silent.

Mechanism: `Daemon.checkDiskSpace` → `safety.DiskFreePercent` → `EnsureProjectTask` (HUMAN assignee) → `sink.Emit(DISK_SPACE_CRITICAL)`. Tests: `internal/queue/disk_watchdog_test.go`. Feature: `internal/queue/features/disk_watchdog.feature`.

### Beat 2.3 — Operator sequence

```sh
make test-e2e   # runs TestJ10_DiskWatchdogDedup against the disk profile
```

The `agentd-disk` service runs with `disk.free_threshold_percent: 100` and a
bind-mounted `@every 5s disk-watchdog` crontab, so the watchdog fires on every
pass. J10 asserts exactly one deduped HUMAN task with one `DISK_SPACE_CRITICAL`
event across repeated passes.

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

**Tickets:** T-014.
**Journey:** J11 in [journeys.md](testing/journeys.md) — `make test-e2e`.

Prove product-plan Phase 2.4: on a repeated failure class, Librarian/FTS surfaces a prior `{symptom, solution}` instead of re-burning tokens. The recall mechanism already exists (`internal/memory/recall.go`); J11 proves it against a live daemon.

### Beat 2.4 — Pass criteria

1. A seeded `{symptom, solution}` USER_PREFERENCE memory is retrievable via `Retriever.Recall` (FTS by intent) when a matching intent arrives.
2. `FormatPreferences` renders the recalled pair into a system prompt block labeled **"USER PREFERENCES"** (lesson-scope `FormatLessons` is exercised in `recall_test.go` with `GLOBAL`/`TASK_CURATION` scopes).
3. Namespace isolation holds: project-scoped memories do not leak across projects.
4. Recall timeout is respected: a slow store returns empty results, not a hang.

Mechanism: `Retriever.Recall` → `Store.RecallMemories` (FTS by intent, scoped to GLOBAL + project + user prefs) → `FormatLessons` / `FormatPreferences`. J11 seeds via `POST /api/v1/preferences` (USER_PREFERENCE scope), then asserts the preference is absent from an earlier task's prompt, present in a later same-user task's prompt, and absent for an unrelated user. Tests: `internal/memory/recall_test.go`, `internal/kanban/memories_repo_test.go`; features: `recall_namespace.feature`, `recall_timeout.feature`.

### Beat 2.4 — Operator sequence

```sh
make test-e2e   # runs TestJ11_PreferenceRecallOnLaterTask against the default profile
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
| Provider fallback (**Beat 2**, above) | Cascade or breaker/HUMAN | T-010a |
| Disk / watchdog (**Beat 2.3**, above) | Durable watchdog signal | T-013 |
| Memory recall | Librarian/FTS on repeat failure | T-014 |
| Permission → HUMAN | Sandbox violation | later encore |

## Related

- Connector inject HUMAN: [demo.md](demo.md)
- Journey suite specs and results: [journeys.md](testing/journeys.md)
- SP-003 run log: SP-003
- SP-004 pre-flight: SP-004
- S02 PR budgets: PR-PLAN.md
