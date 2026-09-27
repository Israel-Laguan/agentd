# E2E Journey Suite (S07)

Status: **under SP-008 spike** (2026-09-28 to 2026-09-29).  
Output of: [SP-008-e2e-journey-inventory.md](../../tasks/sprints/S07-e2e-journeys/spikes/SP-008-e2e-journey-inventory.md)

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

Profiles:
| Profile | When | agentd service | port | config changes | 
| --- | --- | --- | --- | --- |
| default | J01-J06, J13-J15 (standard) | agentd | 8765 | none |
| healing | J07 (connector failure → handoff) | agentd-healing | 8766 | `healing.enabled: true`, `outage_handoff_enabled: true` |
| faults | J08-J09 (provider death, breaker) | agentd-faults | 8767 | `gateway.order: [dead, secondary]` (or at runtime via PATCH) |
| disk | J10 (disk threshold) | agentd-disk | 8768 | `disk.free_threshold_percent: 100`, crontab `@every 5s` |
| tiered | J12 (tiered execution) | agentd-tiered | 8769 | `tiered.enabled: true` |

Implementation: devenv/compose.yaml lists all variants; test setup calls `podman compose up -d <profile>` for the needed service.

### Mock LLM Scenario Selection

**Decision: Inline mock request → scenario tag in messages.**

The mock accepts a request with a special `user` or `system` message tagged with `@scenario=<name>`. Example:

```json
{
  "messages": [
    {"role": "system", "content": "... prompt ..."},
    {"role": "system", "content": "@scenario=cascade-fail-primary"}
  ]
}
```

The mock maintains internal state per scenario (e.g., "fail on primary, succeed on secondary"). Journeys pass the tag as part of their request flow.

Scenarios (T-028 will implement):
| Scenario | Used by | Mock behavior |
| --- | --- | --- |
| success | J01-J07, J13-J15 | return intent + plan/response; no errors |
| cascade-fail-primary | J09 | fail first call, succeed on second (cascade) |
| cascade-fail-all | J09 variant | fail all providers; breaker test |
| breaker-timeout-x5 | J09 | return 4 timeouts, then succeed (open breaker) |
| memory-good | J11 | normal memory ops; include recalled pref in prompt |
| tiered-fail-verify | J12 | small-model plan succeeds, verify fails → escalate |

---

## Journeys

| ID | Title | Steps | Pass criteria | Config | Gotchas | Priority | Coverage |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **J01** | Boot + provider connectivity (warmup on/off) | 1. Start agentd with `--skip-llm-warmup=false`; 2. Verify logs show "LLM warmup OK"; 3. Request `/api/v1/system/status`; 4. Restart with `--skip-llm-warmup=true`; 5. Verify boot succeeds with no warmup log | `system/status` returns 200; warmup logs present when enabled; absent when disabled | default | `devenv` starts with `--skip-llm-warmup`, so manual override needed; warmup on confirms provider reachable at boot | P0 | CP0 |
| **J02** | Board and logs reachable, loop running | 1. Start `make dev-up`; 2. Curl `/api/v1/projects`; 3. Open browser, check kanban loads; 4. Verify SSE `/api/v1/sse` delivers heartbeats | HTTP 200 on board/logs routes; SSE stream delivers events; queue worker loop active (visible in logs) | default | Web routes are client-side tabs, not server routes; SSE is the real data stream | P0 | CP1 |
| **J03** | Chat answers without creating a plan | 1. POST `/api/v1/chat` with a simple intent (no plan needed); 2. Verify response is a chat completion, not a plan | Response is `AIResponse` (chat), not `PlanResponse`; no tasks created | default | Needs a way to signal "chat only" vs "ask for plan"; check feature for current signal | P0 | CP2 |
| **J04** | Chat → plan → approve → materialize → workspace ready → tasks complete | 1. `agentd ask "write hello.txt"`; 2. Approve with Y; 3. Create project workspace dir + README; 4. POST `/workspace/ready`; 5. Watch board as workers claim tasks; 6. Verify tasks COMPLETED | Tasks flow: PENDING → READY → RUNNING → COMPLETED; workspace is non-empty before tasks unlock; all tasks finish | default + mock success scenario | Empty workspace blocks task unlock; demo.md has full script; J04 is the "happy path" | P0 | CP4, demo §2-4 |
| **J05** | Materialization edge cases (not-ready workspace, bad source_path, double ready) | 1. Materialize without workspace; expect 409; 2. Try again with bad `source_path` (non-existent dir); 3. Call workspace/ready twice; verify idempotent or error | Correct HTTP codes (409 for not-ready); idempotent on double-ready; clear error on bad path | default | T-025 closes a gap: failed materialize can leave project row behind | P1 | CP3b |
| **J06** | Task drawer event log shows per-task events | 1. Run J04; 2. Open task detail drawer; 3. Verify `task-started`, `task-claimed`, `task-completed` events in timeline | SSE delivers task-* events; UI renders timeline with correct event sequence | default | Requires browser verification (manual in S07; UI journeys defer to Phase 2) | P1 | CP3a |
| **J07** | Connector failure → HUMAN task → human resolution | 1. Start agentd-healing (healing.enabled: true); 2. Chat → plan → approve with a step that needs a tool call; 3. Force tool to fail (simulated permission denied); 4. Verify HUMAN task created in `_system`; 5. Resolve HUMAN task; 6. Verify next task resumes | HUMAN task created, SSE event sent, next task can be resumed or re-run | healing | healing.enabled: false in dev config (gotcha 2 in spike); need healing config variant; J07 replaces chat-kanban-qa.sh beat 5 | P0 | demo §5, chat-kanban-qa.sh |
| **J08** | Unclean kill mid-task → restart on same home → no stuck RUNNING | 1. Materialize a multi-task plan; 2. Kill -9 agentd while task is RUNNING; 3. Restart agentd on same home; 4. Call `/api/v1/system/status`; 5. Verify no RUNNING tasks; board recovered | No RUNNING tasks after restart; `system/status` returns 200; recovery is automatic (BootReconcile) | default | J08 needs its own agentd (can't share with J07 for timing); Beat 1 (restart-mid-task.sh); T-025 closes gap with new test | P0 | Beat 1 |
| **J09** | Dead primary provider → cascade to secondary; worker failures open the breaker → outage handoff | A. Cascade: 1. Start agentd-faults; 2. Send chat with dead primary in gateway.order; 3. Verify response uses secondary; B. Breaker: 1. Fail worker requests 5 times; 2. Verify breaker OPEN; 3. Verify next task becomes HUMAN | A. ProviderUsed: secondary; B. Breaker OPEN after 5 fails; HUMAN task created | faults | Only queue worker feeds breaker (not chat traffic, so old probe-breaker can never pass); Chat failure is one endpoint, worker failure (via beat2 event) is another | P0 | Beat 2, provider_fallback_test.go, Beat 2.3 (breaker) |
| **J10** | Disk below threshold → one HUMAN "Disk space critical" task, deduped | 1. Start agentd-disk (threshold 100%); 2. Set crontab to `@every 5s`; 3. Wait <10s; 4. Verify one HUMAN task in `_system`; 5. Re-poll crontab; 6. Verify task is not duplicated | Exactly one HUMAN task for disk; SSE event sent; dedup on re-check | disk | Watchdog doesn't run at boot (cron `*/10`); use short interval + bind-mounted crontab for test; gotcha 3 | P0 | Beat 2.3, disk_watchdog_test.go |
| **J11** | Saved preference is recalled and shown to the agent on a later task | 1. Chat → plan → approve; 2. POST `/api/v1/preferences` with a user pref; 3. Start another task; 4. Mock captures request; 5. Verify prompt includes pref text (captured by mock, not in browser) | Pref in USER_PREFERENCE row; recall returns it by user_id + intent; FormatPreferences includes it in prompt | default + memory scenario | No GET for prefs (observable proof is in agent prompt, captured via mock); J11 replaces memory-recall.sh (which was self-fulfilling); T-025 closes gap | P0 | Beat 2.4 |
| **J12** | Tiered execution: small-model plan, escalation on verify failure | 1. Start agentd-tiered; 2. Chat with complex task; 3. Small model makes plan; 4. Verify step fails verification; 5. Escalate to full model | Tiered: small model tried first; verify failure triggers escalation; final step uses full model | tiered | tiered.enabled: false by default; config variant needed; J12 replaces tiered-harness.sh (which only tested fixtures); T-025 removes harness | P1 | Phase 5, tiered-execution.md |
| **J13** | OpenAI-compatible intake (`/v1/chat/completions`) | 1. POST to `/v1/chat/completions` with OpenAI format; 2. Verify response is OpenAI format | Request parsed as OpenAI intake; response format matches OpenAI spec | default | Tested via openai_intake.feature | P1 | openai_intake.feature |
| **J14** | SSE stream delivers task lifecycle events | 1. Open `/api/v1/sse`; 2. Materialize plan; 3. Watch events: `task-started`, `task-claimed`, `task-completed`; 4. Verify order and content | SSE stream is open; all expected events arrive in order | default | results.md lists event names; harness captures via SSE client, not browser | P0 | results.md |
| **J15** | MCP board export | 1. Populate board with tasks; 2. Call `/api/v1/mcp/export` (or similar endpoint); 3. Verify export contains all tasks with IDs, states, outputs | Export JSON includes all task metadata; format matches mcp-board-export.md | default | docs/mcp-board-export.md has format spec | P1 | docs/mcp-board-export.md |

---

## P0 Exit Criteria (must pass on 2 clean runs)

- J01: boot + warmup (2 runs)
- J02: board + SSE (2 runs)
- J03: chat-only response (2 runs)
- J04: full happy path (2 runs)
- J07: HUMAN handoff (2 runs)
- J08: unclean restart recovery (2 runs)
- J09: cascade + breaker (2 runs)
- J10: disk watchdog dedup (2 runs)
- J11: preference recall (2 runs)
- J14: SSE events (2 runs)

---

## P1/P2 Deferral or Bug Policy

For P1/P2 journeys (J05, J06, J12, J13, J15):
- If passing: land them as-is.
- If failing: either (a) defer with a reason in this doc, or (b) open a bug linking the journey.

---

## Todos for T-026, T-027, T-028

**T-026** (harness): Implement test/e2e package with setup/teardown (devenv profile startup, mock scenario injection).

**T-027** (run and triage): Execute all P0 journeys on clean devenv stack twice; triage failures into bugs or deferrals.

**T-028** (mock scenarios): Implement mock LLM scenario selection per table above; ensure cascade, breaker, tiered scenarios work.

