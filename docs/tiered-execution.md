# Tiered execution pipeline

**Status:** design spec (not implemented as a product surface yet)  
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
| Threshold `0` or unset while feature on | Treat as **disabled** (same spirit as `ComplexityThreshold: 0` skipping plan phase today) |

Reuse the existing scoring idea (`EstimateTaskComplexity`: title + description length + newlines) as v1. Later we can add signals (file count hints, labels, Frontdesk plan step count) without changing the gate rule: **below threshold = one worker invocation**.

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
  "budget": {"max_paths": 40, "max_chars": 48000}
}
```

### Invariants

1. Context step **must** write ContextPack to workspace/board storage and attach its id/path on the child tasks.
2. Decision / execute / verify inject the pack; default tool policy **forbids** broad search (repo-wide glob/grep outside `paths`).
3. Re-gather is an **explicit** board action: fail with `NEEDS_CONTEXT`, spawn/redo **context**, bump pack `version`. Never a silent side quest inside execute.
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

v1 can map new step kinds onto existing roles + dedicated **AgentProfile** rows (provider/model pins) without a full UI matrix. Config sketch:

```yaml
tiered:
  enabled: false
  complexity_threshold: 100   # 0 = off; align spirit with agentic.planning.complexity_threshold
  models:
    context:  { provider: ollama, model: "…" }      # small
    decision: { provider: gemini, model: "…" }      # mid
    execute:  { provider: ollama, model: "…" }      # small
    verify:   { provider: gemini, model: "…" }      # mid
    escalate: { provider: anthropic, model: "…" }   # strong
```

Exact keys follow `config.reference.yaml` style when implemented.

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
| M3 | Decision → execute → verify DAG with allowlists |
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

## Open questions

1. Exact threshold default and whether Frontdesk can force `tiered: true|false` per task.
2. Pack storage: workspace file vs SQLite blob vs both.
3. Whether verify should own test command selection or only run decision-specified checks.
4. How agentic inner loop interacts with execute step (likely: execute profile may set `AgenticMode`; context/decision stay JSON/structured).
