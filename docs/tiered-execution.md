# Tiered execution pipeline

**Status:** implemented through M5 (S04 + T-020). Gate, DAG split, step modes, sealed ContextPack, escalation ladder, NEEDS_CONTEXT re-gather and the cost harness are wired and tested. Not yet covered: `tiered.*` config keys for the ladder caps, pack generation numbering, and any wall-clock measurement.  
**Parent plan:** [product-plan.md](product-plan.md) Phase 5  
**Related:** [agentic-harness.md](agentic-harness.md), [architecture.md](architecture.md), existing `EstimateTaskComplexity` / `agentic.planning.complexity_threshold` in `internal/queue/worker/phase_splitter.go` and `internal/config/agentic.go`

## Goal

Spend strong models only where judgment is scarce. For **hard** tasks, split execution into typed steps with different model tiers and **sealed artifacts**, so cheap models gather and apply while mid/strong models decide, verify, and escalate — without relying on “don’t search again” prompts.

Save **money**. Soft prompt discipline is not the mechanism.

On wall-clock: it remains a design goal, not a demonstrated result — see [M5](#m5--costlatency-harness-t-018), which deliberately reports no latency number. Measured cost savings come with a measured token *increase*.

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
3. Re-gather is an **explicit** board action: signal `NEEDS_CONTEXT`, append a fresh **context** chain, rewrite the pack. Never a silent side quest inside execute. (Pack *generation* numbering is not implemented — `version` is the schema version; see the NEEDS_CONTEXT section.)
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

**As implemented (M3):** there is no `tiered.models.<kind>` config key — `internal/config.TieredConfig` / `loadTieredConfig` never read one, and `gateway.role_models` (`RoleModelsConfig`) is a flat one-model-per-role map (`chat` / `worker` / `memory`), not a set of tiers within a role. Step-kind differentiation instead comes from dedicated **AgentProfile** rows named after the step's profile template (`tier-context`, `tier-decision`, `tier-execute`, `tier-verify`, `tier-escalate`). `SplitIntoTieredDAG` (`internal/queue/worker/splitter.go`) stamps each child task's `AgentID` with its profile name; profile lookup resolves the actual provider/model, falling back to the matching `gateway.role_models` entry when a profile leaves provider/model blank — the same fallback relationship documented in `config.reference.yaml`. All five `tier-*` profiles are seeded by `agentd init` (`cmd/agentd/profiles.go`) with empty provider/model so the `gateway.role_models` fallback picks the actual tier models; `cmd/agentd.TestSeededProfilesCoverTieredSteps` fails if a step kind is added without a matching profile.

---

## Escalation ladder

On verify failure:

1. **Flake / obvious fix** — mid model, targeted redo (reuse ideas from `targeted_redo.go`), same pack, then verify again.
2. **Conflict / design ambiguity** — one escalate (strong) pass with pack + failing evidence.
3. **Still blocked** — `HUMAN` (or existing healing / permission handoff paths). Do not spin.

Caps: max 2 mid-fix passes, max 1 escalate. Both are constants in `internal/queue/worker/escalation.go`, counted from the steps on the board so they survive a restart; neither is a config key yet. Always durable board states — no stuck `RUNNING` chat.

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
| M4 | Escalation ladder + HUMAN handoff |
| M5 | Cost/latency harness demo (pairs with product-plan Phase 2) |

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

## M4 — Escalation ladder (T-017)

### Verify outcomes → escalation paths

The **verify** step classifies check results into four outcomes:

| Outcome | Trigger | Response |
| --- | --- | --- |
| **pass** | All checks pass | Mark parent task completed ✓ |
| **flake** | Intermittent failure (transient) | **Mid fix:** bounded redo (execute+verify with same pack) |
| **fail** | Hard failure (repeatable) | **Mid fix:** bounded redo (same pack, different approach) |
| **conflict** | Merge conflict / design ambiguity | **Escalate:** one strong-model pass with evidence, then verify again |

### Bounded mid fix

After verify fail/flake, re-run execute+verify with the same ContextPack. Prevents infinite loops with a cap of **2 passes** (`maxMidFixPasses` in `internal/queue/worker/escalation.go` — a constant today, not yet a `tiered.*` config key).

Passes are counted from the verify steps already on the board, not from a per-task counter, so the cap survives a restart mid-ladder.

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

Escalate step produces a final execution or a bounded redo with corrected strategy. Follow with verify. If escalate+verify still fails, transition to `FAILED_REQUIRES_HUMAN`.

**Caps:** max 1 escalate (`maxEscalations` in `internal/queue/worker/escalation.go` — a constant today, not yet a `tiered.*` config key).

### As implemented (T-020)

Each rung appends a **two-step chain** (`execute → verify`, or `escalate → verify`) to the origin via `PersistTieredDAG`, reusing the same `SPAWNED_BY`/`DEPENDS_ON` shape as the initial split. Appending re-blocks the origin, so the pipeline cannot resolve while a rung is outstanding.

The origin itself is never completed by a step. `PersistTieredDAG` leaves it `BLOCKED`, and `BLOCKED → COMPLETED` is not a legal transition; instead the store unblocks it to `READY` once every child resolves, the queue re-dispatches it, and `tryResolveTieredOrigin` records the pipeline verdict through `UpdateTaskResult`. That same hook stops a finished origin from being split a second time.

### NEEDS_CONTEXT state

If decision/execute/verify step determines the ContextPack is **insufficient** (missing files, misunderstood scope), it transitions to `NEEDS_CONTEXT`:

A decision, execute, or verify step that finds the pack insufficient emits
`{"needs_context": true, "reason": "…"}` instead of its normal output. That
outranks any verdict it would otherwise report.

This is an **explicit board action** — never silent inside execute.

### NEEDS_CONTEXT as implemented (T-020)

1. The signal is read from the step's committed `RESULT` event and recorded as a `TIERED_NEEDS_CONTEXT` event.
2. Stale sibling steps (`PENDING` / `READY` / `QUEUED`) are parked in `NEEDS_CONTEXT` so nothing downstream runs against the old pack. Steps already `RUNNING` are left to finish; their output is superseded.
3. A **fresh `context → decision → execute → verify` chain** is appended to the origin, carrying the stated reason into the new context step.
4. Appending re-blocks the origin, so the pipeline cannot resolve while the re-gather is outstanding.

Two deviations from the sketch above, both deliberate:

- **No edge rewiring.** Rather than repointing existing `DEPENDS_ON` edges (which would need a relation-mutation store method), the new chain depends on the new context step by construction. The abandoned steps stay on the board in `NEEDS_CONTEXT` as a record of what was discarded.
- **No pack generation number.** `ContextPackVersion` is the *schema* version and `Validate()` rejects anything else, so `context_pack.v2.json` would mean "schema v2", not "second attempt". The re-gather rewrites the pack at the same path; the generation is visible on the board as a second context step, not in the filename. Numbering generations needs the pack format to carry a field for it.

`NEEDS_CONTEXT` is a real persisted state: it is in the `tasks.state` CHECK constraint as of schema v16, and `internal/kanban.TestTaskStateCheckConstraintParity` fails if the Go enum and the constraint ever drift apart again.

---

## M5 — Cost/latency harness (T-018)

### Demo script and metrics

Run `./scripts/demo/tiered-harness.sh`. Every number below is derived from the seeded fixtures in `scripts/demo/fixtures/tiered/` — edit a fixture and the numbers move:

| Metric | Baseline (strong) | Tiered | Delta |
| --- | --- | --- | --- |
| **Tokens** | 947 | 2,728 | **2.88× more** |
| **Cost** | $0.0142 | $0.0082 | **42.3% less** |
| **Wall time** | not measured | not measured | see below |

**Tiered execution spends more tokens, not fewer.** Every step re-reads the sealed pack, so total token count goes *up*. Per-step token counts (context=515, decision=647, execute=843, verify=723): **execute is the largest step, followed by verify**, not context — context is in fact the smallest of the four. The saving does not come from routing the largest steps to the small model; execute happens to be both the largest step and small-tier, but verify — the second largest — is priced at the mid tier, and decision, smaller than verify, is also mid tier. The entire saving comes from tier assignment by step kind (context/execute priced small, decision/verify priced mid) regardless of each step's actual size — a price-per-token play, not a token-efficiency play.

Earlier revisions of this table claimed a 34% *token reduction*. That was never measured and is not what the pipeline does — the shape of the win is the opposite. Read the token row as a cost the design pays, not a benefit.

**Fixed task pack:** `scripts/demo/fixtures/tiered/task.json`, reproducible across runs; both arms must satisfy the same acceptance criterion, which the harness reads from the seeded verify verdict rather than asserting.

**Pricing table (offline proxy):**

- Small model: $0.001 / 1k tokens
- Mid model: $0.005 / 1k tokens
- Strong model: $0.015 / 1k tokens

**Output:** JSON results file with the per-step breakdown, token counts, costs, and the token ratio.

### Offline proxy semantics

The harness makes no LLM calls. Token counts come from the actual byte size of the seeded request and response fixtures at a documented 4-bytes-per-token proxy; costs are those counts against the pricing table above.

**Wall-clock latency is deliberately not reported.** Reading fixtures takes microseconds and says nothing about provider latency, so there is no honest offline number to print. Any latency claim needs a live run against real providers. Earlier revisions reported a 38% wall-time win derived from two hardcoded constants; it has been removed rather than restated.

Never report proxy measurements as actual provider costs.

### Success criteria for M5

- ✓ Fixed task pack defined and reproducible
- ✓ Both arms satisfy the same acceptance criterion (seeded verify verdict, enforced by the harness)
- ✓ Cost comparison derived from seeded fixtures, not hardcoded constants
- ✓ Results documented and linked from this spec
- ✓ Script runs offline (no real API calls)
- ✗ Wall-clock comparison — needs a live run; not claimed offline
