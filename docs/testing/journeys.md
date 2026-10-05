# E2E Journey Suite (S07)

Status: **implemented** (17 tests / 14 journeys, green on repeated `make test-e2e` (the target already passes `-count=1`); S07 closed 2026-10-01).  
Output of: `SP-008-e2e-journey-inventory.md` (sprint-local spike, not tracked in git)

---

## Architecture Decisions (SP-008)

### Harness

**Decision: Go package `test/e2e` with `//go:build e2e` flag.**

- Rationale: Existing in-process API tests use httptest; e2e journeys run against a live `devenv/` stack started by test setup.
- Non-goal: Playwright (UI testing comes later); shell (hard to assert without CLI parsing).
- Impact: `make test` (unit + feature tests) stays fast. `make test-e2e` brings the stack up and runs the suite with `-tags=e2e` already set; it is never part of `make test`.
- CI: `.github/workflows/ci-e2e.yml` runs `make test-e2e` weekly on `main`, on demand, and on any pull request that touches `test/e2e/`, `devenv/` or the workflow itself (B-023). The per-PR Go check does not run it, so a change outside those paths is covered by the weekly run, not by its own PR.

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
| breaker | J09 parts B and C (every provider down → breaker trips; provider returns → breaker closes by itself) | agentd-brk | 8770 | `gateway.order: [dead, dead2, flaky]`, `breaker.open_timeout: 10s`, `healing.enabled: true`, `outage_handoff_enabled: true` |
| disk | J10 (disk threshold) | agentd-disk | 8768 | `disk.free_threshold_percent: 100`, crontab `@every 5s disk-watchdog` |
| tiered | J12 (tiered execution) | agentd-tiered | 8769 | `tiered.enabled: true` |
| provider | J16 (siblings wait for a provider breaker's probe) | agentd-prb | 8771 | `gateway.order: [quota]`, `breaker.open_timeout: 10s`, `healing.enabled: true`, `outage_handoff_enabled: true` |

Implementation: devenv/compose.yaml lists all variants; test setup calls `podman compose up -d <profile>` for the needed service, after `make dev-build` has produced the `agentd:local` image those services reference.

**`provider` is separate from `breaker` because a 429 and an outage are different failures.** Only
`ErrLLMQuotaExceeded` reaches the per-provider breakers; an unreachable provider feeds the single global
breaker instead (`internal/queue/worker/worker_handoffs.go`). J09's profile is entirely unreachable
providers, so it can never open a per-provider breaker. The `provider` profile has exactly one gateway
provider on the mock model `prb-quota`, which the journey takes into quota with `POST /quota` and holds
slow with `POST /slow_once`.

**`breaker` is a separate profile from `faults` because they need opposite configurations.** `faults` has a *live* secondary, so a worker call always cascades successfully and the breaker can never open there; `breaker` puts two unreachable providers and a third, `flaky`, in `gateway.order`. `flaky` is a mock model (`brk-outage`) the journey takes down and brings back through the mock LLM's `POST /outage`, so the cascade is walked to exhaustion for the trip and ends in success for the recovery. J09 is therefore split across two journeys — `TestJ09_ProviderCascade` (faults) and `TestJ09_BreakerOpens` (breaker, trip then recovery with no reset call, bounded at the 10s timeout plus 45s) — rather than being one test on one profile.

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
| **J06** | Task drawer renders the task's durable event log | 1. Render `TaskEventList` with events captured from a real `GET /tasks/{id}/events` response; 2. Render `TaskDrawer` with `fetchTaskEvents` mocked; 3. Assert types, payloads, newest-first order, per-task scoping, re-fetch on `eventRefreshKey`, and clearing on a rejected fetch | Every real event type renders with its payload; newest first; only the open task's events; re-reads on refresh; no stale log after a failed read | n/a (vitest + jsdom) | **Redefined 2026-10-01 (T-034):** the old `task-started`/`task-claimed`/`task-completed` timeline cannot pass — claim and start write no event row. No browser tier: layout and click-through are an explicit gap, not backlog. Fixtures captured from a real devenv response | P1 | CP3a |
| **J07** | Connector failure → HUMAN task → human resolution | 1. Start agentd-healing (healing.enabled: true); 2. Chat → plan → approve with a step that needs a tool call; 3. Force tool to fail (simulated permission denied); 4. Verify HUMAN task created in `_system`; 5. Resolve HUMAN task; 6. Verify next task resumes | HUMAN task created, SSE event sent, next task can be resumed or re-run | healing | healing.enabled: false in dev config (gotcha 2 in spike); need healing config variant; J07 replaces chat-kanban-qa.sh beat 5 | P0 | demo §5, chat-kanban-qa.sh |
| **J08** | Unclean kill mid-task → restart on same home → no stuck RUNNING | 1. Materialize a multi-task plan; 2. Kill -9 agentd while task is RUNNING; 3. Restart agentd on same home; 4. Call `/api/v1/system/status`; 5. Verify no RUNNING tasks; board recovered | No RUNNING tasks after restart; `system/status` returns 200; recovery is automatic (BootReconcile) | default | J08 needs its own agentd (can't share with J07 for timing); Beat 1 (restart-mid-task.sh); T-025 closes gap with new test | P0 | Beat 1 |
| **J09** | Dead primary provider → cascade to secondary; worker failures open the breaker → HUMAN handoff | A. Cascade (`TestJ09_ProviderCascade`, faults): 1. Start agentd-faults; 2. Send chat with dead primary first in gateway.order; 3. Verify the request succeeds and the breaker stays CLOSED. B. Breaker (`TestJ09_BreakerOpens`, breaker): 1. Materialize 3 tasks against all-dead providers; 2. Verify breaker OPEN; 3. Verify a HUMAN "Manual review required: AI providers unavailable" child exists; 4. Bring the provider back and verify the breaker CLOSES and a waiting task COMPLETES with no reset call, within the open timeout plus 45s | A. Response succeeds despite an unanswerable first-choice provider, breaker CLOSED; B. Breaker OPEN, HUMAN task created; recovery: breaker CLOSED, task COMPLETED, unaided | faults (A), breaker (B) | Only the queue worker records breaker failures, so chat traffic can't trip it — and chat returns 200 even with every provider dead, so it isn't a usable failure signal either. Trip threshold is 3 (`safety.defaultBreakerFailures`), not 5. `ProviderUsed` has no HTTP surface, so A asserts behaviourally | P0 | Beat 2, provider_fallback_test.go, Beat 2.3 (breaker) |
| **J10** | Disk below threshold → one HUMAN "Disk space critical" task, deduped | 1. Start agentd-disk (threshold 100%); 2. Wait for the watchdog's first pass; 3. Verify one HUMAN task in `_system` with one `DISK_SPACE_CRITICAL` event; 4. Wait out 3 more passes; 5. Verify still exactly one task, same ID, still one event | Exactly one HUMAN task, deduped across passes; exactly one event | disk | Cadence comes from the bind-mounted crontab, not config (`@every 5s`, default `*/10`). Threshold 100% means the watchdog always fires on the container's overlay fs — the journey tests dedup, not a real disk-full | P0 | Beat 2.3, disk_watchdog_test.go |
| **J11** | Saved preference is recalled and shown to the agent on a later task | 1. Materialize + run a project for a user *before* any preference exists; 2. Assert the canary is **absent** from that task's captured prompt; 3. POST `/api/v1/preferences`; 4. Materialize a second project for the same user and run it; 5. Assert the canary is **present** in its prompt; 6. Materialize a third project for an unrelated user and assert it is **absent** | Absent before, present after, absent for another user — i.e. real per-user recall, and no leak into every prompt | default | Phase 1 is the baseline: without it, "present" would be satisfied by anything that always injects prefs. Phase 3 catches a global leak. Needs a prompt-observability channel, which did not exist (see the J11 bug entry in [journey-findings.md](journey-findings.md)) | P0 | Beat 2.4 |
| **J12** | Tiered execution: small-model plan, escalation on verify failure | 1. Start agentd-tiered; 2. Materialize a complex task (title+description ≥ the complexity threshold); 3. Worker splits it into the context/decision/execute/verify DAG; 4. Verify fails; 5. Escalation ladder runs mid-fix redos, then a strong-model escalate completes the origin | Origin reaches COMPLETED; the mock's request capture shows both a verify-step and an escalate-step request | tiered | The mock detects each tiered step from its system-prompt suffix and returns the artifact that step commits; verify fails by default, so the escalation ladder is exercised for real. The `tiered` profile is started by `make dev-up` (added to `COMPOSE_PROFILES`) | P1 | Phase 5, tiered-execution.md |
| **J13** | OpenAI-compatible intake (`/v1/chat/completions`) | 1. POST OpenAI-shaped request → `chat.completion` envelope; 2. declare a `tools` entry → `tool_calls` with `finish_reason: tool_calls`; 3. `tool_choice: "none"` suppresses them; 4. `stream: true` → `chat.completion.chunk` frames + `[DONE]`; 5. error intake → 400 with a stable code | Envelope fields valid (`object`, `chatcmpl-` id, `created`, echoed model, `choices[0]`); tool_calls only for a declared tool; stream framing terminated; 400s for no user message / two approved scopes / undecodable body | default | **Widen 2026-09-30:** the real surface is bigger than "parsed as OpenAI intake" — the handler also accepts `tools`/`tool_choice`/`stream` and emits `tool_calls` only for a tool the client declared. `usage` is declared `omitempty` and never populated on the non-streaming path, so clients must treat it as optional. | P1 | openai_intake.feature |
| **J14** | SSE stream delivers task lifecycle events | 1. Materialize a single-task project; 2. Open a project-scoped `/api/v1/events/stream`; 3. Wait for the task to reach COMPLETED; 4. Drain the stream; 5. Assert `LOG_CHUNK` and `RESULT` both arrived, `LOG_CHUNK` before `RESULT`; 6. Reconcile the live frames against the task's durable event log | Stream open, lifecycle signals present and causally ordered, and live-vs-durable divergence is only the documented `RESULT` case | default | The spec's `task-started` / `task-claimed` / `task-completed` events do not exist — claim and start write no event row. Subscribe *after* materialize: task-dispatch runs every 3s, so listening first would miss a fast task (the durable log covers that window) | P0 | results.md |
| **J15** | MCP board export | 1. `tools/list` → the eight documented board tools with schemas; 2. materialize a project; 3. `board.list_projects` / `board.get_project` return it; 4. `board.list_tasks` returns its tasks, scoped and state-accurate; 5. `board.get_task` returns the detail shape; 6. unknown tool → JSON-RPC `-32602`, missing task → tool error on a 200 | Every advertised tool present with a schema; the project and its tasks exported with real ids/states; the two error shapes distinguishable | default + `mcp.enabled: true` | **Corrected 2026-09-30:** there is no `/api/v1/mcp/export`. The board is a JSON-RPC 2.0 MCP server over Streamable HTTP at `POST /mcp`, and MCP is off by default, so the route is not even registered on a stock config. "Contains all tasks" is false (B-005) and no tool exposes task **outputs** — the export is a state summary. Format now specified in docs/mcp-board-export.md. | P1 | docs/mcp-board-export.md |
| **J16** | Siblings wait for an in-flight provider breaker's probe instead of a handoff | 1. Start agentd-prb and point the default profile at its one provider; 2. Arm the mock's quota for `prb-quota` and materialize three arming tasks; 3. The provider breaker reaches OPEN and the arming tasks are handed off; 4. Quota off, wait out the open timeout, arm `slow_once` and materialize three more tasks; 5. While the breaker is HALF_OPEN, no task emits `PROVIDER_EXHAUSTED_HANDOFF` and no HUMAN subtask appears; 6. All three reach COMPLETED and the breaker is CLOSED | Provider breaker OPEN from real 429s; siblings wait; 3 tasks COMPLETED; breaker CLOSED with no reset after the trip | provider | The three tasks that *trip* a provider breaker are thrown away: the quota branch records the verdict **and** hands the task off, so they end BLOCKED with a HUMAN child. Only a probe admitted after `breaker.open_timeout` can show the sibling behaviour, so the asserted-on tasks are created after the timeout. Also the first end-to-end evidence for B-012 and B-013, which were inert until B-016 | P0 | results.md |

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
J06 was implemented on 2026-10-01 (T-034) as component tests; it had been deferred
as "needs a browser", which held only because the spec wanted events that do not
exist.

Stack bring-up for the non-default profiles. The agentd services take a
prebuilt image, so build it first (`make dev-build`) — a direct compose
invocation cannot build it, and every profile must be named in one invocation
(see the podman-compose gotchas above):

```sh
podman compose -f "$PWD/devenv/compose.yaml" \
  --profile default --profile healing --profile faults \
  --profile breaker --profile disk --profile tiered --profile provider up -d
```

---

## Known Bugs Found by Journeys

Moved to [journey-findings.md](journey-findings.md) on 2026-10-01 (T-033). The
per-journey specs and pass criteria stay here; the historical bug records live
there.

## P1/P2 Deferral or Bug Policy

For P1/P2 journeys (J05, J13, J15; J06 and J12 are implemented):

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

### J06: implemented as component tests (2026-10-01, on T-034)

**Redefined** as "the drawer renders the task's durable event log". The old
`task-started` / `task-claimed` / `task-completed` timeline could never pass:
claim and start write no event row.

Covered by `web/app/components/task/task-event-list.test.tsx` and
`task-drawer.test.tsx` (vitest + jsdom, `cd web && npm test`):

| Test | Asserts |
| --- | --- |
| `TaskEventList` › renders every captured event type | `WARNING`, `RECOVERY`, `TOKEN_USAGE`, `LOG_CHUNK`, `RESULT` all render, with real payloads |
| `TaskEventList` › orders newest event first | the daemon returns oldest-first; the drawer reverses |
| `TaskEventList` › shows an empty state | `No task events yet.` |
| `TaskEventList` › does not mutate the caller's array | the reverse is on a copy |
| `TaskEventList` › marks a truncated payload | `payload_truncated` renders the warning; an empty payload falls back |
| `TaskDrawer` › renders the task's own events | the log comes from `fetchTaskEvents(task.id)` |
| `TaskDrawer` › shows only the open task's events | another task's event is filtered out |
| `TaskDrawer` › re-fetches when `eventRefreshKey` changes | the log does not freeze at first render |
| `TaskDrawer` › clears the list when a refresh rejects | a failed read drops the stale log |
| `TaskDrawer` › does not fetch when no task is open | no spurious request for a closed drawer |

Fixtures are copied from a real `GET /api/v1/tasks/{id}/events` response captured
off the devenv stack on 2026-10-01, pinning the wire shape and payloads (the RESULT
row's id/payload and the drawer fixture's task IDs are adapted for the tests). They
are static snapshots, so they do not detect later drift in the endpoint.

**Explicit gap:** no real-browser tier, so layout and click-through are unproven.
That is a product decision, not a work item, so it is held open as B-027 (S11, P3)
instead of the backlog; B-027 is where "where is the browser tier tracked?" points.

**Decision (SP-015, 2026-10-05): `task-started` / `task-claimed` are not emitted.**
Claim and start are already queryable as state (`started_at`, the task's state) and
as the `LOG_CHUNK` stream, and nothing consumes them: no API, UI or journey reads
such an event, and emitting two rows for every task would grow the event log for
every task to serve a timeline nobody asked for. J06 is therefore neither parked
nor dropped: it stays implemented as redefined above, and the old timeline is
retired with this reason. Re-open only if a consumer needs the transitions as
events.

### J12: implemented (2026-09-30, on T-028)

J12 needed the small model to produce a plan and then *fail verification* on a
known step, so the escalation to the full model is observable; T-028's mock
supplies both halves, as described in that cycle's entry below.
`TestJ12_TieredExecution` materializes a complex task on the `tiered` profile,
waits for the origin to reach COMPLETED, and asserts the mock's request capture
shows both a verify-step and an escalate-step request.

---

## Todos for T-026, T-027, T-028

**T-027** (run and triage): done. P0 journeys pass on repeated clean runs and on 4 consecutive runs against one accumulating stack; P1 journeys J05, J12, J13 and J15 are implemented and passing, and J06 is deferred with written reasons. Defects found and fixed: SQLite per-connection pragmas, J09 profiles, J10 crontab, J11 product gap, J14 unpublished RESULT, B-004 orphan project on a bad `source_path`, B-005, B-006. Filed (now resolved): B-001, B-002 (fixed), B-003 (closed as not a bug; see results.md). `test/e2e/chat-kanban.sh` is deleted now that J04 and J07 pass. Cycle entries are in `results-history.md`.

**T-028** (mock scenarios): done. Request capture (`GET /requests`), the published mock port, per-request scenario selection (in-band `@scenario=` tag, `X-Mock-Scenario` header, or model name via `MOCKLLM_MODEL_SCENARIOS`), error/latency responses, and tiered step replies (each step detected from its system-prompt suffix; verify fails by default) all land. Python unit tests for the dispatch run under `make check` (`devenv/mockllm/test_server.py`). **J12 is the only consumer** of the tiered replies and is now implemented.
