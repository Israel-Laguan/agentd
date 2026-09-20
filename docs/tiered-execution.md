# Tiered execution pipeline

**Status:** implemented in the worker dispatch path (T-017 classifier + T-020 wiring); not yet exposed as a product-facing surface  
**Parent plan:** [product-plan.md](product-plan.md) Phase 5  
**Related:** [agentic-harness.md](agentic-harness.md), [architecture.md](architecture.md), existing `EstimateTaskComplexity` / `agentic.planning.complexity_threshold` in `internal/queue/worker/phase_splitter.go` and `internal/config/agentic.go`

## Goal

Spend strong models only where judgment is scarce. For **hard** tasks, split execution into typed steps with different model tiers and **sealed artifacts**, so cheap models gather and apply while mid/strong models decide, verify, and escalate — without relying on “don’t search again” prompts.

Save **money and wall-clock**. Soft prompt discipline is not the mechanism.

---

## Hard rule: complexity gate

**Simple tasks stay one-shot.** The tiered pipeline must **not** run unless the parent task clears a complexity threshold.

| Condition | Behavior |
| --- | --- |
| `tiered.enabled == false` | Always one-shot (current worker path) |
| Complexity score **below** threshold | One-shot; no DAG split |
| Complexity score **at/above** threshold | Materialize `context → decision → execute → verify` children (and optional escalate) |
| Threshold `0` while feature on | Treat as **disabled** — `0` disables splitting (same spirit as `ComplexityThreshold: 0` skipping plan phase today) |
| Threshold unset while `tiered.enabled: true` | Defaults to `200` (`EstimateTaskComplexity`); split only at/above that score |

Reuse the existing scoring idea (`EstimateTaskComplexity`: title + description length + newlines*10) as v1. Later we can add signals (file count hints, labels, Frontdesk plan step count) without changing the gate rule: **below threshold = one worker invocation**.

Rationale: queue/heartbeat/DAG overhead on tiny tasks can cost more than any model savings.

---

## Pipeline (board-owned, not prompt theater)

Frontdesk / project planning may already have used a strong model. This pipeline is **inside execution** of an approved board task that passed the complexity gate.

Prefer **real DAG children** (`SPAWNED_BY` parent, `DEPENDS_ON` edges), each with a step kind + profile — not one chat that pretends to be four roles.

```text
[parent READY, complex]
        │
        ▼
   context ──► decision ──► execute ──► verify
        │                      ▲           │
        │                      │           ├── pass → parent done
        │                      │           ├── fail → mid fix (bounded) → verify
        │                      │           ├── conflict → escalate (strong) → verify
        │                      │           └── give up → HUMAN
        └── NEEDS_CONTEXT redo (explicit) ─┘
```

| Step | Job | Model tier (default) | Tools |
| --- | --- | --- | --- |
| **context** | Build a sealed **ContextPack** | small / local | read, search, list — **no write** |
| **decision** | Approach, touch list, acceptance checks | mid | pack + optional reads **only on pack paths** |
| **execute** | Apply edits / commands within decision bounds | small | write / bash limited to decision allowlist |
| **verify** | Run checks; classify pass / flake / fail / conflict | mid | test, lint, diff vs pack + decision |
| **escalate** (optional) | Hard conflicts before HUMAN | strong | bounded decision+execute redo; still no silent re-crawl |

Default human workflow mapping: plan (strong, outside) → gather (small) → decide (mid) → execute (small) → fix/verify (mid) → conflict (strong) → HUMAN.

---

## ContextPack (sealed handoff)

The cost win depends on a **versioned artifact**, not on the model “remembering” a ban.

### Required fields (v1 sketch)

```json
{
  "version": 1,
  "task_id": "…",
  "parent_task_id": "…",
  "created_at": "RFC3339",
  "summary": "one short paragraph",
  "paths": ["relative/or/workspace paths in scope"],
  "excerpts": [{"path": "…", "note": "why relevant", "span": "optional"}],
  "commands_run": [{"cmd": "…", "outcome": "…"}],
  "constraints": ["must not…", "API X is source of truth"],
  "unknowns": ["…"],
  "budget": {"max_paths": 40, "max_chars": 48000, "path_count": 1, "char_count": 42}
}
```

`budget.path_count` / `budget.char_count` are populated by `WriteContextPack`;
readers backfill them when absent so packs serialized before the counters became
mandatory still validate.

### Invariants

1. Context step **must** write ContextPack to workspace/board storage and attach its id/path on the child tasks.
2. Decision / execute / verify inject the pack; default tool policy **forbids** broad search (repo-wide glob/grep outside `paths`).
3. Re-gather is an **explicit** board action: fail with `NEEDS_CONTEXT`, spawn/redo **context**, bump the pack generation. Never a silent side quest inside execute.
4. Escalation and mid-fix **keep** the current pack unless a new context task is created.
5. Packs have size limits; overflow goes to `unknowns` or forces a second context pass with a narrower question — not an unbounded dump into the next model.

If the pack is wrong, pay once to rebuild it on purpose. That is measurable.

---

## Model / role routing

Today the gateway already routes by role (`RoleChat`, `RoleWorker`, `RoleMemory`) via `gateway.role_models`. Tiered execution extends that idea:

| Step kind | Suggested role key (new or mapped) | Profile template |
| --- | --- | --- |
| context | `context` (or `memory`-class cheap) | `tier-context` |
| decision | `decision` | `tier-decision` |
| execute | `worker` / `execute` | `tier-execute` |
| verify | `verify` | `tier-verify` |
| escalate | `escalate` or strong `worker` | `tier-escalate` |

**As implemented (M3):** there is no `tiered.models.<kind>` config key — `internal/config.TieredConfig` / `loadTieredConfig` never read one, and `gateway.role_models` (`RoleModelsConfig`) is a flat one-model-per-role map (`chat` / `worker` / `memory`), not a set of tiers within a role. Step-kind differentiation instead comes from dedicated **AgentProfile** rows named after the step's profile template (`tier-context`, `tier-decision`, `tier-execute`, `tier-verify`, plus `tier-escalate` for the one-off strong-model escalation step added in T-020). `SplitIntoTieredDAG` (`internal/queue/worker/splitter.go`) stamps each child task's `AgentID` with its profile name; profile lookup resolves the actual provider/model, falling back to the matching `gateway.role_models` entry when a profile leaves provider/model blank — the same fallback relationship documented in `config.reference.yaml`. Seeding/configuring the `tier-*` profiles themselves is a separate concern from the splitter.

---

## Escalation ladder

On verify failure:

1. **Flake / obvious fix** — mid model, targeted redo (reuse ideas from `targeted_redo.go`), same pack, then verify again.
2. **Conflict / design ambiguity** — one escalate (strong) pass with pack + failing evidence.
3. **Still blocked** — `HUMAN` (or existing healing / permission handoff paths). Do not spin.

Caps: max mid-fix passes, max one escalate unless config says otherwise. Always durable board states — no stuck `RUNNING` chat.

---

## What makes this different

| Common elsewhere | agentd angle |
| --- | --- |
| One model for the whole session | Step-kind model tiers |
| “Use a cheaper model for tools” inside one loop | **Sealed ContextPack** + tool allowlists |
| Personas without handoff contracts | DAG children + board lifecycle |
| Fail in transcript | Fail → fix → escalate → **HUMAN** as first-class states |

Multi-model routing alone is not the story. **Complexity gate + sealed pack + DAG + escalate-to-human** is.

---

## Building blocks already in tree

- Role → provider/model routing (`internal/gateway`, `role_models`)
- Plan-then-execute and complexity threshold (`phase_splitter.go`, `AgenticPlanningConfig`)
- Targeted redo after plan steps (`targeted_redo.go`)
- Workforce profiles, DAG relations, HUMAN / healing handoffs
- Agentic inner loop (opt-in) — stays subordinate; tiered steps may use legacy or agentic mode per profile
- Tiered DAG splitter (`SplitIntoTieredDAG`, `internal/queue/worker/splitter.go`) — pure constructor from a gated parent task to `context → decision → execute → verify` children with `SPAWNED_BY`/`DEPENDS_ON` relations and step-kind profile stamps; persistence and dispatch wiring done in T-016

---

## Non-goals (v1)

- Splitting every task
- Replacing Frontdesk project planning
- SWE-bench chasing via smarter execute models
- Soft-only “please don’t search” policies
- A large operator UI for model matrices before the board pipeline works

---

## Implementation sketch

1. Land this spec; link from [product-plan.md](product-plan.md).
2. Config: `tiered.enabled` + `complexity_threshold` (default off).
3. ContextPack schema + store/load helpers + size limits.
4. Splitter: if gate passes, spawn DAG children with step kinds and profile assignment.
5. Worker modes / tool allowlists per step kind.
6. Escalation + `NEEDS_CONTEXT` transitions wired to existing HUMAN/healing patterns.
7. Metrics: tokens/$ per step kind, re-gather rate, escalate-to-human rate, wall time vs one-shot baseline on a fixed task pack.

### Suggested milestones

| M | Deliverable |
| --- | --- |
| M1 | Gate + config + no behavior change when disabled |
| M2 | ContextPack schema + context worker (read-only) writing pack |
| M3 | Decision → execute → verify DAG with allowlists — splitter done (`SplitIntoTieredDAG`, T-015); step-kind dispatch + tool allowlists done (T-016) |
| M4 | Escalation ladder + HUMAN handoff — classifier/state-machine shipped in T-017; wired into the actual dispatch path in T-020 |
| M5 | Cost/latency harness demo (pairs with product-plan Phase 2) — script shipped in T-018 with hardcoded constants; rebuilt to derive from seeded fixtures in T-020 |

---

## Success criteria

- Below threshold: behavior indistinguishable from today’s one-shot path.
- Above threshold: measurable token/$ and/or wall-time improvement on a fixed hard-task pack vs single mid/strong worker.
- Re-gather rate ≈ 0 except when a new context task is explicitly created.
- Restart mid-pipeline resumes or fails to a clear board state (reliability story).
- Still degrades to HUMAN without a silent loop.

---

## Open questions (resolved — SP-002)

1. **Threshold default:** `tiered.enabled` default `false`; when enabled, `tiered.complexity_threshold` default **`200`** (`EstimateTaskComplexity`). `0` disables splitting. Per-task Frontdesk override = later; v1 config-only.
2. **Pack storage:** workspace file `context_pack.vN.json` + board pointer (path/version). No SQLite blob in v1.
3. **Verify:** runs **decision-specified** checks only; classifies outcomes; does not invent tests.
4. **Agentic mode:** allowed on **execute** (and escalate) profiles only; context/decision stay structured / tool-bounded as specified above.

See [SP-002](../tasks/sprints/S01-positioning-and-demo/spikes/SP-002-tiered-open-questions.md).

---

## M4 — Escalation ladder (T-017, wired in T-020)

### Verify outcomes → escalation paths

The **verify** step classifies check results into four outcomes:

| Outcome | Trigger | Response |
| --- | --- | --- |
| **pass** | All checks pass | Mark parent task completed ✓ |
| **flake** | Intermittent failure (transient) | **Mid fix:** bounded redo (execute+verify with same pack) |
| **fail** | Hard failure (repeatable) | **Mid fix:** bounded redo (same pack, different approach) |
| **conflict** | Merge conflict / design ambiguity | **Escalate:** one strong-model pass with evidence, then verify again |

**As implemented (T-020):** `worker_tiered.go`'s verify step (`processTieredVerifyStep`) parses the model's committed VerifyResult JSON, classifies it via `ClassifyVerifyOutcome`, and calls `handleVerifyOutcome` — this actually drives mid-fix/escalation now, not just the classifier in isolation. Mid-fix and escalate caps are enforced against a durable count (SPAWNED_BY children of the pipeline's origin task), not an in-memory counter, so they survive across separate worker dispatch cycles; `getMetadata`/`setMetadata` route through that same count via `task.Logs` so the metadata API and the persisted count agree.

### Bounded mid fix

After verify fail/flake, re-run execute with the same ContextPack (the redo task reuses AgentID `tier-execute` so it dispatches through the identical tiered execute path — tool allowlist, ContextPack injection, execute prompt — rather than a bespoke one), then verify again. Prevents infinite loops with **configurable cap** (`tiered.escalation.max_mid_fix`, default: max 2 passes).

```text
verify → fail/flake
        ↓
    mid fix (attempt 1)
        ↓
    verify again
        ├─ pass → done
        └─ fail/flake
           ↓
           mid fix (attempt 2)
           ↓
           verify again
           ├─ pass → done
           └─ fail → escalate
```

### Escalation to strong model

On persistent failure or conflict, dispatch **escalate** step (strong model) with:

- Current ContextPack
- Decision artifacts (touch list + checks)
- Failing verify evidence (last check results)

**As implemented (T-020):** the escalate task is a one-off step (`TieredStepEscalate`, AgentID `tier-escalate`) rather than a redo of one of the four fixed DAG kinds — it gets the union of execute+verify tool access (read/write/bash/grep/glob/list) plus the ContextPack and verify evidence embedded in its description, and both fixes the conflict and re-runs checks itself in one pass. `processTieredEscalateStep` reconciles the pipeline's origin task directly: success completes it, failure hands it straight to `FAILED_REQUIRES_HUMAN` — escalation skips the normal retry/healing loop entirely, since it's already the last resort.

**Caps:** max 1 escalate unless config says otherwise (`tiered.escalation.max_escalate`).

### NEEDS_CONTEXT state

If the **decision** step determines the ContextPack is insufficient (missing files, unclear constraint), it outputs `{"needs_context": true, "reason": "..."}` instead of a normal Decision artifact. `processTieredDecisionStep` detects this post-commit (the engine commits the step's output unconditionally; NEEDS_CONTEXT is a judgment made about that already-committed answer, not a different response type) and calls `handleNeedsContext`:

1. The decision task transitions `COMPLETED → NEEDS_CONTEXT` (a transition added specifically for this — see `models.validTaskTransitions`)
2. A new **context** child is spawned (`tier-context`, READY) plus a new **decision** child depending on it (`tier-decision`, PENDING) — the "pack version" is the count of context-kind children spawned so far for this pipeline, not the ContextPack's fixed schema version
3. Any dependent still `PENDING` or `READY` with a `DEPENDS_ON` edge to the stale decision is rewired onto the new decision (`RewireDependsOn`); a `READY` dependent is additionally demoted to `BLOCKED` so it cannot run against the stale pack
4. Once the new decision completes, `reconcileBlockedDependents` re-readies anything parked in `BLOCKED` by step 3 — `UnlockReadyChildren`'s own completion side effect only promotes `PENDING` tasks, so this explicit pass is what actually unblocks it
5. A task already `RUNNING` or beyond when the rewire happens is left alone, per the original spec's "allowed to finish, outputs ignored" allowance

This is an **explicit board action** detected at the decision step — never silent inside execute.

---

## M5 — Cost/latency harness (T-018, rebuilt in T-020)

**As implemented (T-020):** T-018 shipped a harness with `BASELINE_TOKENS=13000`, `BASELINE_WALL_TIME=45`, `TIERED_WALL_TIME=28` hardcoded as bash constants — arithmetic over guessed inputs, not a run of anything. `scripts/demo/tiered-harness.sh` was rewritten to read seeded mock-LLM fixture responses from `scripts/demo/fixtures/{baseline,context,decision,execute,verify}.json` (one file per step, each with `model_tier`, `input_tokens`, `output_tokens`, `duration_ms`, and the actual mock response payload) and derive every total from them via `jq`/`bc`. No aggregate constant is hardcoded in the script; `--fixtures-dir` can point at a different seeded fixture set entirely.

### Demo script and metrics

Run `./scripts/demo/tiered-harness.sh` to compare. With the fixtures committed under `scripts/demo/fixtures/`:

| Metric | Baseline (strong) | Tiered | Savings |
| --- | --- | --- | --- |
| **Tokens** | 13,000 | 9,750 | 25% |
| **Cost** | $0.1950 | $0.0253 | **87%** |
| **Wall time (fixture duration)** | 41,000ms | 25,500ms | 37.8% |

These are the fixture files' seeded numbers as of this writing — re-run the script for the current numbers rather than trusting this table if the fixtures change.

**Pricing table (offline proxy, a rate assumption applied to fixture token counts — not itself measured):**

- Small model: $0.001 / 1k tokens
- Mid model: $0.005 / 1k tokens
- Strong model: $0.015 / 1k tokens

**Output:** JSON results file (`--output`, default `./tiered-harness-results.json`) with per-step breakdown, token counts, cost, wall time, and the verify step's classified outcome.

### Offline proxy semantics

The script never calls a real provider — it reads seeded fixture files and sums them. `duration_ms` in the fixtures is a seeded stand-in for wall time, not a measurement of anything; the output JSON's `note` field says so explicitly, and the script's console output is headed "mock/offline" throughout.

Never report these figures as measured production costs or latencies — they illustrate the tiering shape (cheap models for context/execute, mid models for decision/verify, most token weight where it belongs) under one fixed, reproducible fixture set. A real measurement requires wiring the harness to an actual provider call, which is out of scope here (see T-020's explicit out-of-scope list: "Real (non-mock) LLM cost harness runs against a live provider").

### Success criteria for M5

- ✓ Fixed, seeded fixture set defined and reproducible (`scripts/demo/fixtures/`)
- ✓ Script derives every total from the fixtures — no hardcoded aggregate constants
- ✓ Cost comparison shows measurable token/$ improvement
- ✓ Results documented and linked from this spec
- ✓ Script runs offline (no real API calls)
