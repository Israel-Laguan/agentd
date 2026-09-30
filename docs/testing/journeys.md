# E2E Journey Suite (S07)

Status: **under SP-008 spike** (2026-09-28 to 2026-09-29).  
Output of: `SP-008-e2e-journey-inventory.md` (sprint-local spike, not tracked in git)

---

## Architecture Decisions (SP-008)

### Harness

**Decision: Go package `test/e2e` with `//go:build e2e` flag.**

- Rationale: Existing in-process API tests use httptest; e2e journeys run against a live `devenv/` stack started by test setup.
- Non-goal: Playwright (UI testing comes later); shell (hard to assert without CLI parsing).
- Impact: `make test` (unit + feature tests) stays fast. `make test-e2e` runs the suite only if `-tags=e2e` is passed or `TEST_E2E=1` is set.

**UI journeys:** Browser verification is manual (documented in qa-and-browser-verification.md); not automated in S07.

### Per-Journey Config Mechanism

**Decision: Named `devenv/` service profiles + per-journey environment overrides.**

Rationale:

- The mock LLM (one instance, not switched per journey) handles scenario selection via in-band requests (T-028).
- Daemon-config variants (e.g., healing.enabled, gateway.order, disk threshold) are applied by running a separate agentd service with a distinct config and port.
- `podman-compose` supports `profiles:` on services; we'll use `profiles: ["default", "healing", "faults", "disk", "tiered"]` to conditionally start variants.
- Each journey's test harness names its required profile(s), and the test setup ensures the right services are running.

**Profiles:**

| Profile | When | agentd service | port | config changes |
| --- | --- | --- | --- | --- |
| default | J01-J06, J13-J15 (standard) | agentd | 8765 | none, except `mcp.enabled: true` in `devenv/agentd/config.yaml` for J15 — the product default is off, so the `/mcp` route is not registered without it |
| healing | J07 (connector failure → handoff) | agentd-healing | 8766 | `healing.enabled: true`, `outage_handoff_enabled: true` |
| faults | J09 part A (cascade to a live secondary) | agentd-faults | 8767 | `gateway.order: [dead, secondary]` |
| breaker | J09 part B (every provider dead → breaker trips) | agentd-brk | 8770 | `gateway.order: [dead, dead2]`, `healing.enabled: true`, `outage_handoff_enabled: true` |
| disk | J10 (disk threshold) | agentd-disk | 8768 | `disk.free_threshold_percent: 100`, crontab `@every 5s disk-watchdog` |
| tiered | J12 (tiered execution) | agentd-tiered | 8769 | `tiered.enabled: true` |

Implementation: devenv/compose.yaml lists all variants; test setup calls `podman compose up -d <profile>` for the needed service.

**`breaker` is a separate profile from `faults` because they need opposite configurations.** `faults` has a *live* secondary, so a worker call always cascades successfully and the breaker can never open there; `breaker` puts two unreachable providers in `gateway.order` so the cascade is walked to exhaustion. J09 is therefore split across two journeys — `TestJ09_ProviderCascade` (faults) and `TestJ09_BreakerOpens` (breaker) — rather than being one test on one profile.

### podman-compose gotchas (1.3.0)

- **Every compose invocation must name the profile explicitly** (`--profile X`), including `ps`. `IsRunning` in test/e2e/devenv.go does this; an unqualified invocation sees `services: {}`.
- **`depends_on` only resolves within the activated profiles.** `podman compose --profile breaker up -d` fails with `KeyError: 'litellm'` because litellm is listed under other profiles, not `breaker`. Bring profiles up together: `--profile default --profile breaker`.
- **Bind-mounting a file into the image's home breaks the non-root user.** Podman creates missing parent directories as root, so a crontab mounted at `/home/agentd/.agentd/agentd.crontab` leaves `/home/agentd/.agentd/archives` unwritable and `agentd init` dies with `permission denied`. The `disk` service instead sets `AGENTD_HOME=/home/agentd-disk` (inside its named volume) and mounts the crontab there.
- **`home:` in these devenv configs is ignored.** `internal/config/config.go`'s `newConfigViper` pins `home` to the resolved home dir, so the value in `config.disk.yaml` etc. has no effect; only `db_path`, `projects_dir` and `uploads_dir` are honoured. `AGENTD_HOME` is the supported lever.
- **`-f` must be an absolute path.** podman-compose 1.3.0 `os.chdir()`s before re-opening the file, so a relative path fails with `No such file or directory`. `NewDevenvManager` resolves it via `filepath.Abs`.

### Background-job cadence lives in the crontab, not the config

The disk and outage-handoff jobs are scheduled by `<AGENTD_HOME>/agentd.crontab` (`internal/config/cron.go`), not by a config key. There is **no `disk.check_interval`** — `DiskConfig` carries only `FreeThresholdPercent` (`internal/config/runtime_controls.go`). A journey that needs a fast watchdog must bind-mount a crontab with a short `disk-watchdog` entry; `devenv/agentd/crontab.disk` does this (`@every 5s` vs the default `*/10 * * * *`). An earlier revision of `config.disk.yaml` set a `check_interval: 5s` key that no Go struct reads, so the watchdog silently stayed on the 10-minute schedule and J10 was untestable in an e2e run.

### Mock LLM Scenario Selection

**Decision: per-request scenario selection, by in-band tag, header, or model name.**

`devenv/mockllm/server.py` resolves a scenario for every request, in this order:

1. an in-band `@scenario=<name>` tag in any message (travels with the request, so parallel journeys never share state);
2. an `X-Mock-Scenario` header;
3. the request's `model` name, mapped by `MOCKLLM_MODEL_SCENARIOS` (e.g. `gpt-3.5-turbo=tiered-fail-verify`).

A selected scenario overrides the prompt-inferred behaviour. The tiered
worker cannot inject a tag or header and uses one model for every step, so the
mock also detects each tiered step from its system-prompt suffix
(`TIERED MODE: <STEP> STEP`) and returns the artifact that step commits —
this is what makes J12 a real end-to-end run rather than a fixture.

**Scenarios (T-028, implemented):**

| Scenario | Used by | Mock behavior |
| --- | --- | --- |
| success (default) | J01-J11, J13-J15 | infer from prompt: intent, plan, scope, command; no errors |
| tiered step detection | J12 | return the ContextPack/Decision/execute/verify/escalate artifact for the step's prompt marker; verify fails by default |
| error / error-429 / error-503 | (error-path journeys) | answer with the given HTTP status |
| latency / slow / timeout | (latency journeys) | sleep `MOCKLLM_LATENCY` seconds before answering |

J09's cascade/breaker halves use dedicated all-dead-provider profiles rather
than mock error scenarios, so the mock's error path is not on the P0 critical
path. Python unit tests for the dispatch live in
`devenv/mockllm/test_server.py` and run under `make check`.

---

## Journeys

| ID | Title | Steps | Pass criteria | Config | Gotchas | Priority | Coverage |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **J01** | Boot + provider connectivity (warmup on/off) | 1. Start agentd with `--skip-llm-warmup=false`; 2. Verify logs show "LLM warmup OK"; 3. Request `/api/v1/system/status`; 4. Restart with `--skip-llm-warmup=true`; 5. Verify boot succeeds with no warmup log | `system/status` returns 200; warmup logs present when enabled; absent when disabled | default | `devenv` starts with `--skip-llm-warmup`, so manual override needed; warmup on confirms provider reachable at boot | P0 | CP0 |
| **J02** | Board and logs reachable, loop running | 1. Start `make dev-up`; 2. Curl `/api/v1/projects`; 3. Open browser, check kanban loads; 4. Verify SSE `/api/v1/sse` delivers heartbeats | HTTP 200 on board/logs routes; SSE stream delivers events; queue worker loop active (visible in logs) | default | Web routes are client-side tabs, not server routes; SSE is the real data stream | P0 | CP1 |
| **J03** | Chat answers without creating a plan | 1. POST `/api/v1/chat` with a simple intent (no plan needed); 2. Verify response is a chat completion, not a plan | Response is `AIResponse` (chat), not `PlanResponse`; no tasks created | default | Needs a way to signal "chat only" vs "ask for plan"; check feature for current signal | P0 | CP2 |
| **J04** | Chat → plan → approve → materialize → workspace ready → tasks complete | 1. `agentd ask "write hello.txt"`; 2. Approve with Y; 3. Create project workspace dir + README; 4. POST `/workspace/ready`; 5. Watch board as workers claim tasks; 6. Verify tasks COMPLETED | Tasks flow: PENDING → READY → RUNNING → COMPLETED; workspace is non-empty before tasks unlock; all tasks finish | default + mock success scenario | Empty workspace blocks task unlock; demo.md has full script; J04 is the "happy path" | P0 | CP4, demo §2-4 |
| **J05** | Materialization edge cases (not-ready workspace, bad source_path, double ready) | 1. Materialize with neither `source_path` nor `start_empty_workspace` → 201 with PENDING tasks; 2. `workspace/ready` before seeding → 409; 3. seed, `workspace/ready` → 200; 4. `workspace/ready` again → 200, same set; 5. bad `source_path` (missing dir; and a file) → 400, no project row; 6. good `source_path` → 201, seeded, root tasks READY | 409 `STATE_CONFLICT` from `workspace/ready` on an empty workspace; double-ready idempotent; bad `source_path` 400 with nothing persisted | default | **Corrected 2026-09-30:** materialize never 409s — it accepts the plan and returns PENDING tasks. The 409 is `workspace/ready`'s. The old "409 for not-ready" step was written against an API that does not exist. | P1 | CP3b |
| **J06** | Task drawer event log shows per-task events | 1. Run J04; 2. Open task detail drawer; 3. Verify `task-started`, `task-claimed`, `task-completed` events in timeline | SSE delivers task-* events; UI renders timeline with correct event sequence | default | Requires browser verification (manual in S07; UI journeys defer to Phase 2) | P1 | CP3a |
| **J07** | Connector failure → HUMAN task → human resolution | 1. Start agentd-healing (healing.enabled: true); 2. Chat → plan → approve with a step that needs a tool call; 3. Force tool to fail (simulated permission denied); 4. Verify HUMAN task created in `_system`; 5. Resolve HUMAN task; 6. Verify next task resumes | HUMAN task created, SSE event sent, next task can be resumed or re-run | healing | healing.enabled: false in dev config (gotcha 2 in spike); need healing config variant; J07 replaces chat-kanban-qa.sh beat 5 | P0 | demo §5, chat-kanban-qa.sh |
| **J08** | Unclean kill mid-task → restart on same home → no stuck RUNNING | 1. Materialize a multi-task plan; 2. Kill -9 agentd while task is RUNNING; 3. Restart agentd on same home; 4. Call `/api/v1/system/status`; 5. Verify no RUNNING tasks; board recovered | No RUNNING tasks after restart; `system/status` returns 200; recovery is automatic (BootReconcile) | default | J08 needs its own agentd (can't share with J07 for timing); Beat 1 (restart-mid-task.sh); T-025 closes gap with new test | P0 | Beat 1 |
| **J09** | Dead primary provider → cascade to secondary; worker failures open the breaker → HUMAN handoff | A. Cascade (`TestJ09_ProviderCascade`, faults): 1. Start agentd-faults; 2. Send chat with dead primary first in gateway.order; 3. Verify the request succeeds and the breaker stays CLOSED. B. Breaker (`TestJ09_BreakerOpens`, breaker): 1. Materialize 3 tasks against all-dead providers; 2. Verify breaker OPEN; 3. Verify a HUMAN "Manual review required: AI providers unavailable" child exists | A. Response succeeds despite an unanswerable first-choice provider, breaker CLOSED; B. Breaker OPEN, HUMAN task created | faults (A), breaker (B) | Only the queue worker records breaker failures, so chat traffic can't trip it — and chat returns 200 even with every provider dead, so it isn't a usable failure signal either. Trip threshold is 3 (`safety.defaultBreakerFailures`), not 5. `ProviderUsed` has no HTTP surface, so A asserts behaviourally | P0 | Beat 2, provider_fallback_test.go, Beat 2.3 (breaker) |
| **J10** | Disk below threshold → one HUMAN "Disk space critical" task, deduped | 1. Start agentd-disk (threshold 100%); 2. Wait for the watchdog's first pass; 3. Verify one HUMAN task in `_system` with one `DISK_SPACE_CRITICAL` event; 4. Wait out 3 more passes; 5. Verify still exactly one task, same ID, still one event | Exactly one HUMAN task, deduped across passes; exactly one event | disk | Cadence comes from the bind-mounted crontab, not config (`@every 5s`, default `*/10`). Threshold 100% means the watchdog always fires on the container's overlay fs — the journey tests dedup, not a real disk-full | P0 | Beat 2.3, disk_watchdog_test.go |
| **J11** | Saved preference is recalled and shown to the agent on a later task | 1. Materialize + run a project for a user *before* any preference exists; 2. Assert the canary is **absent** from that task's captured prompt; 3. POST `/api/v1/preferences`; 4. Materialize a second project for the same user and run it; 5. Assert the canary is **present** in its prompt; 6. Materialize a third project for an unrelated user and assert it is **absent** | Absent before, present after, absent for another user — i.e. real per-user recall, and no leak into every prompt | default | Phase 1 is the baseline: without it, "present" would be satisfied by anything that always injects prefs. Phase 3 catches a global leak. Needs a prompt-observability channel, which did not exist (see the J11 bug entry) | P0 | Beat 2.4 |
| **J12** | Tiered execution: small-model plan, escalation on verify failure | 1. Start agentd-tiered; 2. Materialize a complex task (title+description ≥ the complexity threshold); 3. Worker splits it into the context/decision/execute/verify DAG; 4. Verify fails; 5. Escalation ladder runs mid-fix redos, then a strong-model escalate completes the origin | Origin reaches COMPLETED; the mock's request capture shows both a verify-step and an escalate-step request | tiered | The mock detects each tiered step from its system-prompt suffix and returns the artifact that step commits; verify fails by default, so the escalation ladder is exercised for real. The `tiered` profile is started by `make dev-up` (added to `COMPOSE_PROFILES`) | P1 | Phase 5, tiered-execution.md |
| **J13** | OpenAI-compatible intake (`/v1/chat/completions`) | 1. POST OpenAI-shaped request → `chat.completion` envelope; 2. declare a `tools` entry → `tool_calls` with `finish_reason: tool_calls`; 3. `tool_choice: "none"` suppresses them; 4. `stream: true` → `chat.completion.chunk` frames + `[DONE]`; 5. error intake → 400 with a stable code | Envelope fields valid (`object`, `chatcmpl-` id, `created`, echoed model, `choices[0]`); tool_calls only for a declared tool; stream framing terminated; 400s for no user message / two approved scopes / undecodable body | default | **Widen 2026-09-30:** the real surface is bigger than "parsed as OpenAI intake" — the handler also accepts `tools`/`tool_choice`/`stream` and emits `tool_calls` only for a tool the client declared. `usage` is declared `omitempty` and never populated on the non-streaming path, so clients must treat it as optional. | P1 | openai_intake.feature |
| **J14** | SSE stream delivers task lifecycle events | 1. Materialize a single-task project; 2. Open a project-scoped `/api/v1/events/stream`; 3. Wait for the task to reach COMPLETED; 4. Drain the stream; 5. Assert `LOG_CHUNK` and `RESULT` both arrived, `LOG_CHUNK` before `RESULT`; 6. Reconcile the live frames against the task's durable event log | Stream open, lifecycle signals present and causally ordered, and live-vs-durable divergence is only the documented `RESULT` case | default | The spec's `task-started` / `task-claimed` / `task-completed` events do not exist — claim and start write no event row. Subscribe *after* materialize: task-dispatch runs every 3s, so listening first would miss a fast task (the durable log covers that window) | P0 | results.md |
| **J15** | MCP board export | 1. `tools/list` → the eight documented board tools with schemas; 2. materialize a project; 3. `board.list_projects` / `board.get_project` return it; 4. `board.list_tasks` returns its tasks, scoped and state-accurate; 5. `board.get_task` returns the detail shape; 6. unknown tool → JSON-RPC `-32602`, missing task → tool error on a 200 | Every advertised tool present with a schema; the project and its tasks exported with real ids/states; the two error shapes distinguishable | default + `mcp.enabled: true` | **Corrected 2026-09-30:** there is no `/api/v1/mcp/export`. The board is a JSON-RPC 2.0 MCP server over Streamable HTTP at `POST /mcp`, and MCP is off by default, so the route is not even registered on a stock config. "Contains all tasks" is false (B-005) and no tool exposes task **outputs** — the export is a state summary. Format now specified in docs/mcp-board-export.md. | P1 | docs/mcp-board-export.md |

---

## P0 Exit Criteria (must pass on 2 clean runs)

- J01: boot + warmup (2 runs)
- J02: board + SSE (2 runs)
- J03: chat-only response (2 runs)
- J04: full happy path (2 runs)
- J07: HUMAN handoff (2 runs)
- J08: unclean restart recovery (2 runs)
- J09: cascade (faults) + breaker trip (breaker) (2 runs)
- J10: disk watchdog dedup (2 runs)
- J11: preference recall (2 runs)
- J14: SSE events (2 runs)

## T-026 Status: Complete

All eleven P0 journeys (10 distinct journeys, J09 is two tests) are
implemented and pass under stress: **J01, J02, J03, J04, J07, J08, J09
(cascade + breaker), J10, J11, J14**. Verified 4 consecutive runs on one
stack with zero failures, zero SQLITE_BUSY, and TOKEN_USAGE rows durable
(0→26 projects, 0→29 tasks, 0→76 token events).

Discovered and fixed four real defects:

- **SQLite SQLITE_BUSY silent writes** — pragmas applied once to the pool,
  leaving 5/6 connections with 0ms timeout. Write drops under contention,
  including TOKEN_USAGE rows and thus under-reported token spend. Fixed by
  moving pragmas into DSN parameters; verified by
  `TestOpenAppliesBusyTimeoutToEveryConnection` and 4-run stress test.
- **J09** — the spec's single-profile design was impossible (cascade needs a
  live secondary, a breaker trip needs every provider dead). Split into two
  profiles. Also corrected the trip threshold (3, not 5) and the assumption
  that chat failure is a usable signal (it is not — chat returns 200 even
  with every provider dead).
- **J10** — `disk.check_interval` was a dead config key; the cadence comes
  from the crontab. Fixture bug, fixed with a bind-mounted crontab.
- **J11** — preferences were recalled in principle but structurally
  unreachable by the worker. Needed a product change (project-level
  `user_id`, schema v19) plus a prompt-observability channel.
- **J14** — a completed task's `RESULT` event was persisted but never
  published to the event bus, so live subscribers never learned a task
  finished. Product bug, fixed.

P1 journeys (J05, J13, J15) were implemented in the 2026-09-30 cycle and pass.
J12 was implemented on 2026-09-30 once T-028 landed the tiered mock replies.
J06 remains deferred (browser tier) with its reason below.

Stack bring-up for the non-default profiles:

```sh
podman compose -f "$PWD/devenv/compose.yaml" \
  --profile default --profile healing --profile faults \
  --profile breaker --profile disk up -d
```

All profiles must be named in one invocation — see the podman-compose gotchas
above.

---

## Known Bugs Found by Journeys

### J08: boot reconcile misses tasks owned by a PID-1 daemon

`MarkTaskRunning` stamps `os.Getpid()` (the daemon's own PID) on a RUNNING
task. `queue.BootReconcile` resets a RUNNING task only when its owning PID is
no longer alive. In the devenv container agentd is PID 1 before and after a
restart, so the previous owner always appears alive and the interrupted task
is skipped at boot. It is only recovered by the stale-heartbeat sweep
(`StaleAfter`, 2m default; observed ~2m after restart). `TestJ08_UncleanKillRecovery`
therefore allows up to 180s and logs time-to-recovery. Fix options (product
decision, not yet made): stamp a per-boot instance ID alongside the PID, or
have boot reconcile treat any RUNNING task started before this daemon's boot
as a ghost. Once fixed, tighten the test to a short deadline.

### J10 (fixed in the devenv fixture, not product code): the disk watchdog's cadence was unreachable from config

`devenv/agentd/config.disk.yaml` set `disk.check_interval: 5s`, but no Go
config struct reads that key — `DiskConfig` (`internal/config/runtime_controls.go`)
carries only `FreeThresholdPercent`, and the watchdog's interval comes from the
`disk-watchdog` line in `<AGENTD_HOME>/agentd.crontab`
(`internal/config/cron.go`'s `applyCronJob`). The key was silently ignored, so
the watchdog stayed on the default `*/10 * * * *` and J10 could not observe a
pass (let alone a deduped second one) inside an e2e run.

Fixed by adding `devenv/agentd/crontab.disk` (an `@every 5s disk-watchdog`
entry) and bind-mounting it into the `agentd-disk` service, and by deleting the
dead key. This is a fixture bug, not a product bug — but it is worth flagging
that **`disk.check_interval` reads like a real knob and isn't one**. If
operators are expected to tune the watchdog cadence, the key should either be
implemented or the config reference should say the crontab is the only input.
No product change made; the choice is a product decision.

### J14: task completions were never published to the event bus (product bug, fixed)

`RESULT` — the event that reports a task's outcome, and the one the web UI's
live board needs to move a card to done — was appended inside the store's DB
transaction (`Store.UpdateTaskResult` → `kanban/db.AppendTaskResultEvent` via
`FinishTaskResultSideEffects`, in the same transaction that flips the task to
COMPLETED). Durable, yes, but it never passed through `bus.EventEmitter.Emit`,
so it was never published to the bus.

SSE subscribers therefore saw a task begin producing output and then go
silent; completion only became visible on a later poll or reconnect. Verified
directly: a task's durable log held a `RESULT` row while the stream carried
only `[WARNING TOKEN_USAGE LOG_CHUNK]`.

Fixed by adding `models.EventBroadcaster` (publish-without-persisting,
implemented by factoring the fan-out out of `Emit`) and broadcasting the
RESULT from the worker's commit path. It must not be re-persisted — that
would double the event-log rows.

Also corrected here: the spec's `task-started` / `task-claimed` /
`task-completed` events do not exist. `ClaimNextReadyTasks` and the
READY → RUNNING transition write no event row, so there is nothing to stream
for them. J14 asserts the events that do exist.

### SQLite drops events under contention (product bug, fixed)

Running the full suite repeatedly against one long-lived stack made J04/J14
fail intermittently — a task completed but its `LOG_CHUNK` / `TOKEN_USAGE`
events were missing from both the stream *and* the durable log. The daemon logs
showed why:

```text
ERROR failed to persist token usage ... err="add token usage: database is locked (5) (SQLITE_BUSY)"
WARN  memory touch failed ... err="touch memories: database is locked (5) (SQLITE_BUSY)"
```

**Root cause.** `internal/kanban/db/open.go` applied `busy_timeout` and
`foreign_keys` with `db.ExecContext` on the connection pool. Both are
*per-connection* settings, so only the one connection that happened to run
them got them; every other pooled connection had a 0ms busy timeout and failed
instantly under write contention. The retry wrapper (`RetryOnBusy`, ~150ms
total) could not compensate. A completed task could therefore be missing the
rows describing how it got there, and a dropped `TOKEN_USAGE` row means the
token ledger under-reports spend.

**Fix.** The pragmas are now passed as `_pragma=` DSN parameters, so the driver
applies them to every connection (`connectionDSN`, covered by
`TestOpenAppliesBusyTimeoutToEveryConnection`, which fails without the fix).
Foreign-key enforcement, previously silently off on most connections, now
applies everywhere.

Contention comes from two suite artefacts:

- **Task accumulation and state drift.** J08 SIGKILLs the daemon mid-task; its
  recovery marks the killed task complete but leaves a sibling RUNNING for 60s.
  Each run accumulates more projects, tasks, and a small number of long-lived
  RUNNING/FAILED states. Across 4 runs: projects grow 0→26, tasks 0→29.
- **No board cleanup.** Accumulated projects are never deleted (no DELETE route),
  so the dispatch loop and status summarizer scan more rows with each run.

Verified: 4 consecutive test runs on one stack (0→26 projects, zero SQLITE_BUSY,
TOKEN_USAGE rows durable and monotonic) confirm the pragma fix eliminates the
write drops under real contention. J04/J14 remain stable across runs.

For clean-database runs, use `make dev-clean dev-up` (drops volumes); plain
`make dev-down` keeps the named volumes and accumulates state for stress
testing. Retrying or queueing on a still-busy DB remains a product decision
and is not implemented here.

### Suite hygiene: breaker poisoning, test cache, readiness

- **Startup race.** `up -d` returns once containers exist, and podman-compose
  does not reliably honour `depends_on: service_healthy`, so early tasks could
  reach litellm while it still refused connections. Three failures trip the
  circuit breaker (`defaultBreakerFailures`) and block dispatch for
  `defaultBreakerTimeout` (5m), leaving every later journey stuck in READY.
  `make dev-up` now waits for all containers to report healthy, and J11 resets
  the breaker like J07/J09 do.
- **Go test cache.** `make test-e2e` passes `-count=1`; without it a repeat run
  reports `ok (cached)` in 0s, which would falsely satisfy "2 clean runs".

### J11: preferences could not reach a worker prompt (product gap, fixed)

Four independent breaks, each sufficient on its own to make the journey's
goal unreachable — the last one only surfaced once the others were fixed, on
the second run against a stack that already held preferences from a previous
run. Verified on the live stack: `POST /api/v1/preferences`
correctly writes a `USER_PREFERENCE` memory row, and recall *with* a
`user_id` returns it — but:

1. The worker's execution-time recall passed `userID=""`
   (`internal/queue/worker/worker_messages.go`), so `RecallMemories` never
   added its `USER_PREFERENCE` branch to the query. A blank user is an
   exclusion, not a wildcard.
2. `memoryFormatLessons` deliberately skips `USER_PREFERENCE` rows, and
   `memory.FormatPreferences` was never called from the worker — so even
   recalled preferences were dropped before the prompt.
3. Nothing carried a user identity to execution: `models.Task` had no
   `user_id`, and phase continuations, breakdowns and handoff children are
   all created long after the chat turn.
4. Once the first three were fixed, a user with more preferences than
   `RecallTopK` (default 5) still lost the newest ones: as a branch of the
   bm25-ranked FTS query they competed with lessons for the same LIMIT, so
   saving a preference could silently stop having any effect. Fixed by
   recalling preferences in their own newest-first query, under their own
   ceiling, with no requirement that they match the task's intent terms.

Fixed by stamping the requesting user on the **project** (new
`projects.user_id`, schema v19) rather than per task, so tasks created later
inherit it for free; reading `X-Agentd-User` on materialize (the header chat
already uses, and preferred over a body-supplied value); and appending
`memory.FormatPreferences(recalled)` as its own system message after the
stable prefix, so anonymous prompts are byte-identical to before.

There was also no way to *observe* the prompt: the devenv mock logged only
correlation metadata, which is how the old memory-recall demo script (deleted
in T-025) ended up simulating its own success in Python. The mock now appends
every request body to a JSONL log, served back at `GET /requests`.

### J09 (fixed in the devenv fixture): the faults profile could never trip the breaker

The journey spec assumed one `faults` profile could cover both cascade and
breaker. It can't: cascade requires a *live* secondary, and tripping the
breaker requires *every* provider dead. With a working secondary, a worker
request always succeeds via cascade, so no failure is ever recorded. Split into
two profiles (`faults` for the cascade, new `breaker` for the trip) rather
than weakening either assertion.

Two further spec corrections, both verified against the running stack:

- **The trip threshold is 3, not 5.** `internal/queue/safety/defaultBreakerFailures`
  is 3, and it is a constant with no config override. The test asserts the
  observable contract (breaker OPEN + HUMAN child) rather than a count, so
  retuning the threshold won't break it.
- **`/v1/chat/completions` returns HTTP 200 even when every provider is dead**,
  answering with a `"[SYSTEM] Communication with AI core timed out"` message.
  So chat is not a usable failure signal for the breaker half. Confirmed
  directly against the all-dead `breaker` profile on :8770.

---

## P1/P2 Deferral or Bug Policy

For P1/P2 journeys (J05, J06, J12, J13, J15):

- If passing: land them as-is.
- If failing: either (a) defer with a reason in this doc, or (b) open a bug linking the journey.

### J05, J13, J15: implemented (2026-09-30)

All three pass on the default profile with no new mock scenarios. Each needed
its spec corrected against the running stack first — J05's "409 on materialize",
J13's narrow "parsed as OpenAI intake", and J15's `/api/v1/mcp/export` endpoint
and "with outputs" promise all described behaviour the product does not have.
The corrected rows are in the table above; the J15 response format is now
actually specified in `docs/mcp-board-export.md`.

Defects found: B-004 (bad `source_path` returned 500 and orphaned the project —
**fixed** in the same cycle), B-005 (`board.list_tasks` silently caps at 100
tasks), B-006 (`board.list_tasks` ignores `state` when `project_id` is passed).
B-005 and B-006 are **fixed** in the housekeeping cycle; J15 now exports a
>100-task board completely and honours `state` with and without `project_id`.

### J06: deferred to Phase 2 (UI journeys need a browser)

J06 asserts that the task drawer renders a `task-started` / `task-claimed` /
`task-completed` timeline. Two of those three events do not exist at all:
`ClaimNextReadyTasks` and the READY → RUNNING transition write no event row
(the same finding J14 already corrected), so there is nothing for the timeline
to show and nothing to stream. What remains is a rendering assertion about
React components, which needs a browser driver — deliberately out of scope for
this Go harness (see the SP-008 harness decision: "Non-goal: Playwright"). The
durable half is already covered by J14.

Re-entry condition: a browser-based test tier exists, and the three
task-lifecycle events are implemented if the timeline is meant to show them.

### J12: implemented (2026-09-30, on T-028)

J12 needed the small model to produce a plan and then *fail verification* on a
known step, so the escalation to the full model is observable. T-028's mock
now detects each tiered step from its system-prompt suffix and returns the
artifact that step commits; the verify step fails by default, so the
escalation ladder (bounded mid-fix redos, then a strong-model escalate) runs
for real and completes the origin. `TestJ12_TieredExecution` materializes a
complex task on the `tiered` profile, waits for the origin to reach COMPLETED,
and asserts the mock's request capture shows both a verify-step and an
escalate-step request. The `tiered` profile is started by `make dev-up`.

---

## Todos for T-026, T-027, T-028

**T-027** (run and triage): done. P0 journeys pass on repeated clean runs and on 4 consecutive runs against one accumulating stack; P1 journeys J05, J12, J13 and J15 are implemented and passing, and J06 is deferred with written reasons. Defects found and fixed: SQLite per-connection pragmas, J09 profiles, J10 crontab, J11 product gap, J14 unpublished RESULT, B-004 orphan project on a bad `source_path`, B-005, B-006. Filed but not fixed: B-001, B-002, B-003. `test/e2e/chat-kanban.sh` is deleted now that J04 and J07 pass. Cycle entries are in `results.md`.

**T-028** (mock scenarios): done. Request capture (`GET /requests`), the published mock port, per-request scenario selection (in-band `@scenario=` tag, `X-Mock-Scenario` header, or model name via `MOCKLLM_MODEL_SCENARIOS`), error/latency responses, and tiered step replies (each step detected from its system-prompt suffix; verify fails by default) all land. Python unit tests for the dispatch run under `make check` (`devenv/mockllm/test_server.py`). **J12 is the only consumer** of the tiered replies and is now implemented.
