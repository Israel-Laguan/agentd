# agentd product plan

**Status:** draft from competitive positioning review (2026-09-14)  
**Scope:** make the governance + durability edge visible; do not chase “best coding agent.”  
**Related:** [README](../README.md), [architecture](architecture.md), [agentic harness](agentic-harness.md), [agentic harness roadmap](agentic-harness-roadmap.md) (inner loop — largely complete), [tasks/ sprint board](../tasks/README.md).

---

## One-line positioning

> Local daemon that turns an approved plan into a durable Kanban board and runs sandboxed workers against it — model-agnostic, SQLite source of truth.

Put this in GitHub **About**, README hero, and any landing page. Do not lead with “AI coding agent” or “Kanban for Claude.”

---

## What we are (and are not)

| We are | We are not |
| --- | --- |
| Local-first **control-plane harness** (board + workers + memory) | Another chat loop that edits files |
| Durable task lifecycle with human gates | A SWE-bench / editor UX competitor |
| House daemon for unattended, degradable runs | A thin wrapper that only drives Claude Code |

**Category slot:** self-hosted work runtime for multi-step projects with approval, cheap/local models, and failure → human tickets.

**Closest cousins (for contrast, not clone):** agent-kanban, HAR, harness-kanban, Open Agent Harness — they wrap or orchestrate *someone else’s* loop; we own the board **and** the workers.

---

## Edge to protect (do not dilute)

1. **Board-first** — SQLite task lifecycle / DAG is source of truth, not the chat transcript.
2. **Approval before autonomy** — Frontdesk plan → human commit → materialize.
3. **Failure as board state** — permission / outage → `HUMAN` / system tasks, not a stuck session.
4. **Memory pipeline** — archive → curated memories → FTS → dream consolidation.
5. **Workforce** — profiles, registry, reassign / split / retry.
6. **Provider reality** — Gemini-first, Ollama / llama.cpp / Horde, cascade `gateway.order`.
7. **Go daemon packaging** — single binary, SQLite, cron, non-root Docker.
8. **Written invariants** — schema, transitions, optimistic locking, atomic claim ([foundational baseline](architecture.md#foundational-baseline-contract)).

---

## Risks if we stay fuzzy

- Read as “yet another agent framework.”
- Collapsed into the crowded “local Kanban for agents” cluster without a sharper story.
- Weak GitHub discoverability (About / topics / website).
- No public harness-reliability story while peers argue Terminal-Bench / Harness-Bench.

---

## Plan phases

### Phase 0 — Positioning surface (docs / repo metadata)

**Goal:** strangers get the slot in under 30 seconds.

| # | Action | Done when |
| --- | --- | --- |
| 0.1 | Set GitHub About description to the one-liner above | About field matches one-liner |
| 0.2 | Add topics: `agent-harness`, `local-first`, `kanban`, `sqlite`, `multi-agent`, `golang` | Topics visible on repo |
| 0.3 | README hero: one-liner + 3-bullet “what / not / why local” | First screen answers “what is this?” |
| 0.4 | Add `docs/why-agentd.md` (or README section): contrast vs Claude Code, OpenHands, agent-kanban, HAR | Table lives in-repo and is linked from README |
| 0.5 | Optional: short website or docs landing that repeats the same one-liner | Public URL in About (if/when ready) |

**Non-goals:** rewriting architecture docs; changing runtime behavior.

---

### Phase 1 — 10-minute demo path

**Goal:** the default story is *governance*, not “it wrote a REST API.”

Script (document in README or `docs/demo.md`):

1. `make build` → `./bin/agentd init`
2. Configure a cheap path (e.g. Gemini-only or Ollama) per README first-run
3. `agentd ask` (or Frontdesk intake) → produce a plan
4. **Human approves** → materialize workspace + board tasks
5. `agentd start` → watch board / SSE as workers claim work
6. Force or show one **permission failure** becoming a `HUMAN` task
7. Restart mid-run (optional beat) and show durable claim / resume

| # | Action | Done when |
| --- | --- | --- |
| 1.1 | Write `docs/demo.md` with exact commands and expected board states | New contributor can run it cold |
| 1.2 | Add a “10-minute path” link near README Quickstart | Linked from hero / quickstart |
| 1.3 | Capture one short recording or annotated screenshot of board + HUMAN handoff | Asset linked from demo doc |
| 1.4 | Smoke checklist in CI or `make demo-smoke` (where feasible without live LLM) | Documented offline / mock path or skip rules |

---

### Phase 2 — Harness reliability story (not SWE-bench)

**Goal:** prove the *house*, not the coding model.

| # | Demo / artifact | Pass criteria |
| --- | --- | --- |
| 2.1 | Restart mid-task | Task resumes or fails cleanly to a board state; no silent stuck `RUNNING` |
| 2.2 | Provider fallback | Kill primary provider; cascade in `gateway.order` continues work or opens system/human task |
| 2.3 | Disk / resource watchdog | Watchdog surfaces a durable event or task (no silent death) |
| 2.4 | Memory recall on repeat failure | Librarian / FTS surfaces a prior `{symptom, solution}` on a repeated class of failure |
| 2.5 | Circuit breaker on LLM outage | Breaker trips; board shows outage / healing behavior per config |

Package as `docs/harness-reliability.md` + a small task pack (scripts or fixtures) that others can run. Prefer this over chasing SWE-bench leaderboards.

---

### Phase 3 — Distribution wedge (optional, high leverage)

**Goal:** steal “wrap existing agents” distribution without giving up the control plane.

| # | Action | Done when |
| --- | --- | --- |
| 3.1 | Design MCP **export** of the board (tasks, states, comments) | Spec in `docs/` |
| 3.2 | Prototype MCP server so Claude Code / Cline can act as a *worker behind* agentd | Read path works; write path gated |
| 3.3 | Document “agentd owns board; external agent is a worker” | Contrast table updated |

Keep ownership clear: board + approval + lifecycle stay in agentd.

---

### Phase 4 — Product hardening (ongoing, board-aligned)

Prioritize work that reinforces the edge; deprioritize “better single-session coding UX.”

**Keep investing in**

- Frontdesk approval UX and materialize clarity
- Queue: claim, heartbeat, retry, deadlines, breaker
- Sandbox → HUMAN task path
- Librarian memory quality and “dream” consolidation
- Workforce manager: reassign / split / retry
- Cheap/local provider story (Ollama, llama.cpp, Horde, OpenAI-compatible)

**Deprioritize / refuse**

- Positioning as Claude Code / OpenHands competitor
- Primary KPI = SWE-bench
- Features that make the transcript the source of truth again

Inner agentic loop work is tracked separately in [agentic-harness-roadmap.md](agentic-harness-roadmap.md) (MVP complete). New coding-loop work should stay opt-in and subordinate to board invariants.

---

## Suggested sequencing

```text
Phase 0 (positioning) ──► Phase 1 (10-min demo) ──► Phase 2 (reliability pack)
                                      │
                                      └──► Phase 3 (MCP export) when distribution matters
```

Phase 4 runs continuously; do not block demos on it.

---

## Success metrics (lightweight)

| Signal | Target |
| --- | --- |
| First-visit comprehension | Cold reader can restate the one-liner after README hero |
| Demo completion | Unfamiliar operator finishes Phase 1 script in ~10 minutes |
| Category clarity | Issues/PRs talk about board / approval / workers, not “another Cursor” |
| Reliability pack | At least 2.1–2.3 runnable and documented |

---

## Immediate next actions (done in S01 — kept for history)

1. ✓ Done: Apply Phase 0.1–0.4 (About, topics, README hero, `docs/why-agentd.md` contrast).
2. ✓ Done: Author `docs/demo.md` (Phase 1.1) from the current Quickstart + Frontdesk + HUMAN-task path.
3. ✓ Done: Pick one reliability beat — **restart mid-task** (SP-001 → T-006/`docs/harness-reliability.md`); connector HUMAN stays in `docs/demo.md`.

---

## Out of scope for this plan

- Rewriting the foundational baseline contract
- Choosing a single “best” commercial model
- Full public benchmark suite beyond the harness reliability pack

---

## Phase 5 — Tiered execution pipeline (cost wedge)

**Status:** design bet — not shipped as a product surface yet.  
**Spec:** [tiered-execution.md](tiered-execution.md)  
**Hard rule:** complexity gate — simple tasks stay one-shot; the pipeline runs only at/above threshold.
**Goal:** spend strong models only where judgment is scarce; keep gathering and mechanical edits on small/cheap models. Save money and wall-clock without relying on “please don’t search again” prompts.

### Pipeline shape

For a board task that is already planned (Frontdesk / materialize done), split **execution** into typed substeps — preferably real DAG children, not one chat that role-plays phases:

| Step | Job | Typical model tier | Tools allowed |
| --- | --- | --- | --- |
| **context** | Gather facts into a sealed pack (paths, APIs, constraints, open questions) | small / local | read, search, list — **no write** |
| **decision** | Choose approach, file touch list, acceptance checks | mid | read pack only (optional narrow read) |
| **execute** | Apply the decision (edits / commands) | small | write / bash within decision bounds |
| **verify** | Run checks; classify fail vs flake vs conflict | mid | test / lint / diff against pack |
| **escalate** (optional) | Hard conflicts before HUMAN | strong | bounded redo of decision+execute |

Default mapping matches how people already work: plan with strong (already outside this pipeline), gather with small, decide with mid, execute with small, fix/verify with mid, give up upward.

### Why this can be unique

Most harnesses either (a) one model for the whole session, or (b) route by agent persona without **sealed handoffs**. agentd can make each step a board task with profile + role routing (`RoleWorker` / new roles like `context` / `decision` / `verify`) and durable artifacts. That is governance again: the board owns the pipeline, not the transcript.

You already have building blocks:

- Gateway **role → provider/model** routing (`RoleChat`, `RoleWorker`, `RoleMemory`)
- Agentic **plan phase** then execute (`phase_splitter.go`)
- Workforce **profiles** and DAG (`DEPENDS_ON` / `SPAWNED_BY`)
- Failure → **HUMAN** / healing paths

Missing piece: treat context/decision/execute/verify as **first-class step kinds** with artifact contracts, not prompt sections inside one worker call.

### Feasibility: “don’t collect context again”

Soft instruction fails often. Models re-explore when uncertain, tools are available, or the pack is incomplete.

**Hard rule (required for the cost win):**

1. **Context step** must emit a versioned **ContextPack** artifact (files touched candidates, excerpts, commands run, unknowns). Store it on the board/workspace and point child tasks at it.
2. **Decision / execute / verify** start with the pack injected; their tool allowlists **forbid** broad search by default (or allow only paths listed in the pack).
3. Re-gather is an **explicit board action**: `context` redo or `NEEDS_CONTEXT` state — never a silent side quest inside execute.
4. Verify failures escalate: mid fix → strong conflict pass → HUMAN — still without wiping the pack unless a new context task is spawned.

If the pack is wrong, you pay once to rebuild it on purpose. That is cheaper and more measurable than hoping the model “remembers” a verbal ban.

### Risks

- **Pack quality:** a bad small-model context pack poisons the pipeline; need pack schema + size limits + “unknowns” field.
- **Over-splitting:** tiny tasks pay more in queue/heartbeat overhead than they save in tokens — gate the pipeline on complexity (reuse `EstimateTaskComplexity` / planning threshold ideas).
- **Role explosion:** start with four step kinds and profile templates; don’t invent a model matrix UI first.
- **False uniqueness:** “multi-model routing” alone is common; **sealed pack + DAG + escalate-to-human** is the differentiator — document it that way.

### Implementation sketch (when we build it)

1. Spec landed: [tiered-execution.md](tiered-execution.md) (ContextPack, step kinds, allowlists, escalation, complexity gate).
2. Extend role routing (or profile templates) for `context` / `decision` / `execute` / `verify`.
3. Materialize or phase-split complex READY tasks into DAG children with `DEPENDS_ON`.
4. Worker modes: context-only / decision-only / execute-bounded / verify.
5. Metrics: tokens per step kind, re-gather rate, escalate-to-human rate, wall time vs single-model baseline.

### Success criteria

- Measurable token/$ drop on a fixed task pack vs single mid/strong worker.
- Re-gather rate near zero unless a new context task is created.
- Still degrades to HUMAN without a stuck chat loop.

### Sequencing note

Do **after** Phase 0–1 (positioning + demo). Can parallel Phase 2 (reliability) once the ContextPack contract exists — restart mid-pipeline is a killer reliability demo.
