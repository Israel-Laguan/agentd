# SP-003: Demo path dry-run — connector control → HUMAN

| Field | Value |
| --- | --- |
| Type | spike |
| Status | done |
| Priority | P0 |
| Sprint | S01-positioning-and-demo |
| Time box | 2–4 hours |
| Links | [US-002](../stories/US-002-ten-minute-demo.md), [T-004](../tasks/T-004-demo-doc.md), [llm-connector-strategy](../../../docs/llm-connector-strategy.md), [frontdesk](../../../docs/frontdesk.md) |

## Question

Can we (1) run ask→approve→board with a connector we control, then (2) **inject a deliberate provider failure** so the board shows human intervention — without hoping a real sandbox permission pops up?

## Prerequisite: how the LLM connector works (reviewed)

agentd’s model is **two topologies** ([`docs/llm-connector-strategy.md`](../../../docs/llm-connector-strategy.md)):

```text
Direct:  agentd ──HTTP──▶ openai-compatible endpoint (OpenAI / Gemini-openai / Ollama / llama.cpp / LiteLLM)
Managed: agentd ──HTTP──▶ LiteLLM (etc.) ──▶ N providers
```

**Keep in agentd:** tools, truncation, task token budgets, **cascade**, **circuit breaker**, **role routing** (`chat` / `worker` / `memory` via `gateway.role_models`).  
**Delegate:** keys, account quotas, provider RPM, wire translation for exotic APIs.

**Routing precedence:** explicit caller `Provider` → `gateway.role_models[role]` → `gateway.order` cascade.

**Breaker-relevant errors only:** `ErrLLMUnreachable`, `ErrLLMQuotaExceeded` (`internal/queue/safety/breaker.go`). Other gateway errors take a different failure path.

**Human intervention via connector (what we will force):**

| Mechanism | Where | What you see on the board |
| --- | --- | --- |
| **Provider-exhausted handoff** | `Worker.HandleGatewayError` → breaker open / quota → `createProviderExhaustedHandoff` | Parent **BLOCKED**; HUMAN child titled `Manual review required: AI providers unavailable`; event `PROVIDER_EXHAUSTED_HANDOFF` |
| **System outage handoff** | Daemon `checkOutageHandoff` after breaker open ≥ `breaker.handoff_after` (default `2m`) | System project HUMAN task: `System Offline: Please check AI API connections.`; event `LLM_OUTAGE_HANDOFF` |
| Healing **off** | `healing.enabled: false` | Same errors → terminal `FAILED` (no HUMAN child) — **bad for the demo**; keep healing on |

Config knobs: `gateway.order`, `gateway.providers.*.base_url` / `adapter`, `healing.enabled`, `healing.outage_handoff_enabled`, `breaker.handoff_after`.

**Not the primary injection for this spike:** sandbox `sudo` / permission → HUMAN (real, but harder to script as “connector control”). Optional encore after connector path works.

## Method (control the connector)

Use a **dedicated AGENTD_HOME** (e.g. `/tmp/agentd-sp003`) so prod config stays clean.

### Phase A — Happy path (cheap real or local endpoint)

1. Point `gateway.order` at one working provider you control (Gemini-only, Ollama, or LiteLLM).
2. `init` → `ask` / chat → DraftPlan → approve → materialize → `start`.
3. Confirm board shows worker progress (SSE optional).

### Phase B — Inject failure on purpose (connector)

Pick **one** primary recipe (B1 recommended):

**B1 — Dead base_url (fastest, fully controlled)**

1. After a task is `READY`/`RUNNING` (or before next LLM call), rewrite config so the **only** entry in `gateway.order` uses `adapter: openai` and `base_url: "http://127.0.0.1:1"` (nothing listening) **or** stop Ollama / LiteLLM mid-run.
2. Ensure `healing.enabled: true` (default).
3. Optionally shorten `breaker.handoff_after` (e.g. `10s`) so system outage HUMAN appears without a 2m wait.
4. Trigger another LLM call (worker tick / ask). Cascade has nowhere to go → `ErrLLMUnreachable` → breaker → **Manual review** HUMAN child and/or **System Offline** task.

**B2 — Single-provider cascade, then kill upstream**

1. `gateway.order: [ollama]` (or litellm only).
2. Run until workers need the LLM; stop/kill the upstream process.
3. Same handoff path as B1.

**B3 — Quota-class error (if injectable)**

Only if you can make the endpoint return a quota/429 that maps to `ErrLLMQuotaExceeded`. Prefer B1 unless you already have a stub that returns that.

### Phase C — Observe & record

- [ ] Parent state `BLOCKED` (provider handoff) and/or system HUMAN outage task
- [ ] Child title prefix `Manual review required:`
- [ ] Events: `PROVIDER_EXHAUSTED_HANDOFF` and/or `LLM_OUTAGE_HANDOFF`
- [ ] Exact config diff used to inject
- [ ] Restore working connector; note whether completing the HUMAN child unblocks the parent

## Output

- [ ] Command + config transcript for A → B → C
- [ ] Screenshots or `curl` board/task dumps proving HUMAN
- [ ] Gaps: doc-only vs code bugs (file `B-xxx` if needed)
- [ ] Go/no-go for T-004: demo script uses **connector inject** as the HUMAN beat (not “hope for sudo”)

## Out of scope

Fixing cascade/breaker bugs unless they block the demo; tiered execution; full reliability pack.

## Notes

- README already warns: set `healing.enabled: false` / `outage_handoff_enabled: false` to **suppress** these handoffs — do the opposite for this spike.
- Existing tests cover breaker classification and handoff helpers; this spike is an **ops dry-run**, not a new unit test suite. Optional follow-up: a scripted e2e under `e2e/` or `scripts/` that points at a dead URL.


## Run log (2026-09-14)

**Home:** `/tmp/agentd-sp003` (throwaway; not `~/.agentd`)  
**Evidence:** `/tmp/agentd-sp003/evidence/`  
**Go/no-go for T-004:** **GO** — demo HUMAN beat should be **connector inject** (`base_url` → dead port), not sudo/permission roulette.

### Connector notes

- Real Gemini: key present via LiteLLM env, but stock `gemini-2.5-flash` returns 404 for this key; `gemini-3.6-flash` works but frontdesk plan call was too slow/unreliable for the spike window.
- Used a **local mock OpenAI** on `127.0.0.1:18080` as the controlled connector (`gateway.order: [openai]`, `gateway.openai.base_url: http://127.0.0.1:18080/v1`).
- Workspace tasks stay `PENDING` until workspace is non-empty + `POST /api/v1/projects/{id}/workspace/ready` (empty → 409). Document this in `docs/demo.md`.

### Phase A — happy path

1. `agentd --home /tmp/agentd-sp003 init`
2. Mock OpenAI + config pointing openai → mock; `start --skip-llm-warmup` on `:18765`
3. `printf y | agentd --home … ask "Create a tiny project that writes hello.txt…" --api-url http://127.0.0.1:18765`
4. Result: Proposed project `hello-txt` → approved → **project started**
5. Seeded `projects/<id>/README.md`, then `workspace/ready` → task `READY`

### Phase B — inject failure

**B-accidental (mock up):** Worker got non-command JSON from the naive mock → healing ladder exhausted → HUMAN child  
`Manual review required: self-healing failed`; parent `Write hello.txt` → `BLOCKED`.  
Useful, but **not** the connector story.

**B-intentional (dead connector):** Rewrote config to `base_url: http://127.0.0.1:1/v1`, restarted daemon, materialized `dead-connector-demo`, seeded + ready.

Observed within ~10s:

| Signal | Value |
| --- | --- |
| Breaker | `OPEN`, `failure_count: 3` |
| last_error | `dial tcp 127.0.0.1:1: connect: connection refused` (wrapped `ErrLLMUnreachable`) |
| HUMAN child | `Manual review required: AI providers unavailable` (`READY`, assignee `HUMAN`) |
| Parent | `Touch marker.txt` → `BLOCKED` |
| Event path | `createProviderExhaustedHandoff` / provider-exhausted handoff |

System Offline (`LLM_OUTAGE_HANDOFF`) did **not** appear in project list within ~25s of OPEN (may need longer `handoff_after` observation or different project filter); **provider-exhausted HUMAN is enough for the demo beat**.

### Phase C — checklist

- [x] Command + config transcript (this run log + `/tmp/agentd-sp003/config.yaml` history)
- [x] Board dumps in `evidence/` (`tasks-human-healing.json`, `poll-dead.json`, `final-tasks.json`)
- [x] Gaps noted (Gemini default model stale; workspace empty 409; mock must speak intent/scope/plan/**worker** JSON; System Offline not confirmed)
- [x] Go/no-go: **GO** for T-004 with dead-`base_url` HUMAN beat

### Demo recipe to document in T-004

1. Controlled working connector (mock or pinned working Gemini model).
2. ask → approve → seed workspace → `workspace/ready`.
3. Flip connector to unreachable (`127.0.0.1:1` or kill upstream); keep `healing.enabled: true`.
4. Show breaker OPEN + HUMAN `Manual review required: AI providers unavailable` + parent BLOCKED.
