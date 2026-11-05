# The agent-kit boundary (SP-024)

What belongs in a library that agentd's controller imports, what gets deleted, and what has to be cut first. Result of spike SP-024 (S19), built on the S17 keep/outsource map and the T-042 runs in [acp-agents.md](acp-agents.md) and [acp-presets.md](acp-presets.md). The kit is **in-repo first** (`agentkit/`, T-045); a separate module or repo is gated by the criteria at the end.

**Status (2026-11-05):** decided for the gateway, the stage seam, context building and the leaf packages. **The builtin loop is undecided**: the one comparison that exists is one run per cell, plus one extra run for cline on the small task, with opencode missing. Its cuts are planned both ways below, with the run that would settle it.

## Starting data (re-measured 2026-11-05, `35dcec02`)

```sh
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/agent/... ./internal/gateway/... \
  | tr ' ' '\n' | grep '^agentd/internal' | sort | uniq -c | sort -rn
```

**Gateway** (before PR 10 cut it). 118 files (67 tests), 4,493 non-test lines. Non-test code still imports `internal/models` from 5 packages (`gateway`, `correction`, `providers`, `routing`, `spec`) and `internal/api/correlation` from 2 (`providers`, `routing`). Nothing else. **285 files outside `internal/gateway` import it or a subpackage**, up from 269 on 2026-10-31 (PRs 02–08 added callers). A path move cannot fit in one PR.

**Agent packages** (non-test lines; importers are files outside `internal/agent/`):

| Package | Files / lines | Importers | agentd imports (besides the gateway) |
| --- | --- | --- | --- |
| `agent/context` | 13 / 2,153 | 36 | `config`, `models`, **`agent/runtime`, `agent/session`** |
| `agent/filecontext` | 5 / 618 | 6 | `paths` |
| `agent/hooks` | 9 / 1,076 | 55 | `models`, `sandbox`, `toolenv`, **`agent/tools`, `agent/session`** |
| `agent/runtime` | 7 / 1,007 | 39 | `config`, `models`, `api/correlation`, `agent/hooks`, `agent/tools` |
| `agent/session` | 2 / 286 | 15 | none |
| `agent/skills` | 2 / 376 | 5 | `markdown`, `paths` |
| `agent/subagent` | 4 / 961 | 3 | `capabilities`, `markdown`, `sandbox`, `agent/context`, `agent/hooks`, `agent/tools` |
| `agent/tools` | 15 / 1,913 | 72 | `capabilities`, `config`, `models`, `paths`, `sandbox`, `toolenv`, `agent/filecontext` |
| `queue/worker/agentic` | 12 / 1,809 | — | the loop itself |

Two facts that the first table missed, because it only looked outside `agent/`:

- **`agent/context` is not a clean *move*.** Its only imports of `runtime` and `session` come from `message_editor.go`, the history editor for checkpoint rewind. Its only users are the agentic loop (`iteration.go`, `rewind.go`) and the worker's construction of it. The other 12 files have no loop dependency.
- **Everything the builtin loop needs hangs off `tools` and `hooks`.** `subagent` imports `context`, `hooks` and `tools`; `runtime` imports `hooks` and `tools`. `hooks` is not loop-only: the ACP path calls it from `queue/worker/acp_permission.go`, so it stays in agentd whichever way the loop goes.

## Kit contents

| Package | Call | Evidence |
| --- | --- | --- |
| `gateway` and subpackages | **move** | S17 map "keep (kit)". L1 was the cheapest method for intake and task creation (about 2,000 tokens, against 22,000 for opencode, SP-022). Five symbols and one leaf separate it from zero agentd imports |
| `stage` (`stage.go`, `contract.go`, `model_agent.go`) | **move**; the public seam | Imports only the gateway. `ModelAgent` is the L1 agent that every non-execution stage uses |
| `stage/intake` | **stays** | Controller logic, uses board draft types |
| `ContextPack` type | **move the type**; `contextpack_io.go` stays | Imports only the stdlib. Writing it under a task directory is agentd's business |
| `agent/context` **minus `message_editor.go`** | **move** | agentd builds the context for any agent. `message_editor.go` goes with the loop (see below) |
| `agent/filecontext` | **move** | Same |
| `api/correlation`, `paths` | **move** (leaves) | stdlib-only; 15 and 11 importers |
| `aicalls` | **move after the gateway** (optional) | One importer; needs a kit record type in place of `models.AICall` |
| `agent/hooks` | **stays in agentd** | agentd's policy (denylist, credentials, audit), and ACP permission requests go through it |
| `sandbox`, `models`, `config` | **stay** | The jail serves both agent kinds (SP-025); `models` and `config` get kit-owned types and option structs, mapped in agentd |
| `agent/runtime`, `agent/tools`, `queue/worker/agentic` | **undecided** | See "The builtin loop" |
| `agent/session` checkpoints, `agent/context/message_editor.go` | **follow the loop** | Used only by the rewind. `secret_store.go` stays with hooks |
| `agent/subagent` | **follows the loop** (owner, 2026-11-05) | Only the loop calls it (3 importers, all `queue/worker`), and it imports `context`, `hooks` and `tools`. No measured stage used it. It is live: `delegate` and `delegate_parallel` are in every agentic turn's tool list |
| `agent/skills` | **follows the loop** | Skills reach only the builtin system prompt (`enrichBuilderMatchedSkills`). Nothing in `acp*.go` or `contextpack*.go` mentions them. The S17 reason ("not implemented under `agentic`") was wrong: the unimplemented key is `agentic.skills`; skills are read from `queue.skills`. Deleting them is a behaviour change for anyone with a skills directory |
| `capabilities`, `toolenv`, `markdown` | **follow the loop** | `capabilities` is imported by `subagent`, `tools`, `capabilities/plugin` and the worker. `markdown` is used only by `skills` and `subagent` |

**No read cache, no "trust your writes" prompt.** SP-027: repeated reads were 17.5% of `exec.turn` input, but each read is billed once, because the cut drops it right after. The model re-reads because the cut also dropped its own write. That is a context-cut question (T-059), not a kit feature.

## The builtin loop

### What is known

- **Cost.** SP-022: the loop finished clear tasks at 9,000–23,000 tokens, against 23,000+ for opencode. The loop's `ModelAgent` stays either way (it is L1, not the loop).
- **Reliability, SP-022 (3 runs per cell):** on the ambiguous task the loop reported COMPLETED with nothing done in 2 of 3; on the small task one of 3 wrote a failing test and reported success. The builtin's sandbox could not see the Go toolchain in those runs.
- **Data loss, SP-027 / B-089.** On the default 12,000-char budget the cut deleted code in 5 of 12 workspaces, 2 of them COMPLETED. **Fixed** (S19 PR 02): a whole-file `write` to a file whose latest read was cut is refused. The kit inherits that fix if `tools` moves.
- **The live smoke, S19 PR 08 (one run per cell except cline/small, which recorded 2 runs, [acp-presets.md](acp-presets.md#live-smoke-2026-11-05)):**

  | | small | multi-file | ambiguous |
  | --- | --- | --- | --- |
  | cline | COMPLETED, `go test` passes, **behavior check fails** (`CountLines("")` returns 1) | COMPLETED, same behavior failure | asked a human |
  | builtin (B-089 fixed) | asked a human before writing | asked a human **mid-change**; `CountWords` lost and `count.go` missing its `strings` import, so `go test` fails | asked a human |
  | opencode | not measured: free tier used up | | |

### Why it is not decided

1. **Sample too small, and opencode is missing.** The best ACP candidate (opencode: go, SP-022) has no run. The smoke above is n=1 per cell, except cline/small at n=2; retries, if any, are not separated from first attempts in the record.
2. **The two seams do not start alike.** The builtin path runs pre-task elicitation (skipped only for a description over 200 characters with success criteria or constraints); an `acp` route skips it. The small task is 152 characters with no criteria, so only the builtin was asked to clarify, and it never wrote code.
3. **Neither side is good.** The builtin left a broken workspace when it asked a human mid-change; cline passed its own tests with a wrong `CountLines`. A COMPLETED state is not evidence without a workspace check.

### What would settle it

One more live session (needs the owner's go-ahead for each run and a free model on the ACP session):

- opencode on the same three tasks, once its free tier resets;
- **tasks with success criteria**, so both seams start alike;
- **n ≥ 3 first-attempt runs per cell** (retries excluded from n, reported separately), the builtin with B-089 fixed and the Go toolchain bound into its jail;
- the same host-side check on every workspace: each file against the seed (size, function list), `go test`, and the behavior test.

**Decision rule, proposed before the runs:**

- **Move** if the builtin matches the best ACP agent on the workspace checks (clear tasks) and keeps its token advantage (SP-022: at most half the tokens).
- **Delete** if it loses or breaks code where an ACP agent does not, or if an ACP agent passes the checks on every clear task.
- **Move takes precedence** when both agents pass every clear-task check and the builtin uses at most half the tokens — both rules above would fire, and the tie goes to Move.
- Anything else: keep it where it is, out of the kit, and do not spend a PR on it.

### If it moves: an L3 `stage.Agent` in the kit

The loop's own cuts, after steps 1–11. None has been sized on a built branch.

| Step | What | Note |
| --- | --- | --- |
| L1 | **Move `message_editor.go` into `agent/runtime`.** Runtime then imports `context`, which no longer imports it | Do this before step 9, so `agent/context` is clean. Check for a cycle through `session` |
| L2 | **Hook interface.** The kit takes `PreHook`, `PostHook`, `ToolCallEvent` and `ToolResultEvent`; agentd's denylist, credential and audit hooks implement it | `hooks` imports `tools` for names and result types; `tool_aliases.go` has to point the other way |
| L3 | **`session` checkpoints** (`secret_store.go` stays) | 15 importers |
| L4 | **`tools`:** `sandbox` becomes a kit `Executor` interface; `capabilities` becomes an interface; `config` becomes `Options`; `models` types become kit types | **72 importers**: move with an alias facade, then migrate in batches |
| L5 | **`runtime`:** `config` → `Options`, `models.AgentProfile` → a kit profile, `ErrBudgetExceeded` from `agentkit/llm` | 39 importers |
| L6 | **`agentic`** (12 files) moves with a kit `TaskInput` | |
| L7 | **`subagent`, `skills`:** move, and fix B-057 (list the loaded names in the schema, or make `subagent` an enum) | Depends on L2 and L4 |

### If it is deleted

| Step | What | Note |
| --- | --- | --- |
| D1 | **Config.** `agentic_mode`, the `queue.agentic_*` keys and `queue.skills` become load-time "removed key" errors (the S18 PR 07 mechanism), with a docs note | The deprecation is the user-visible part |
| D2 | **Delete `subagent` and `skills`** | The builtin agent's tool list shrinks (`delegate`, `delegate_parallel`); B-057 is dropped |
| D3 | **Delete the loop:** `queue/worker/agentic`, `runtime`, `session` checkpoints, `message_editor.go`, the engine wiring in `worker.go` | 71 files outside these packages import them, 42 of them tests |
| D4 | **Cut `tools`.** `hooks` needs `ToolResult`, `ToolStatus` and the tool-name constants; keep those in `hooks` and delete the rest. `sandbox` stays | |

**Cost to name before deciding:** with the loop gone, `exec.turn` needs an external agent on the host's `PATH`. There is no fallback for a user without one, and the tests that drive the loop have to move to the fake ACP agent or go.

## Dependency cut plan

Decided part. Step sizes were set on 2026-10-31 and still hold, apart from the importer counts above.

**Moves keep an alias facade at the old path** (type aliases, `const`s, `var X = new.X`, a wrapper for `GenerateJSON[T]`), so importers can switch over in any batch. Every move PR also re-keys `tools/checkminfunc/baseline`, updates `purpose_sites_test.go` and the depguard globs. The size limit is 100 files and 2,000 changed lines per PR; T-045 caps its own at 1,000.

| Step | PR | What | Size (e) |
| --- | --- | --- | --- |
| 1 | PR 10, done | **The errors leaf.** New `agentkit/llm` holds `ErrLLMUnreachable`, `ErrLLMQuotaExceeded`, `ErrInvalidJSONResponse`, `ErrBudgetExceeded` and `UsageDetails`; `models` aliases them, so the 69 files using `models.Err*` do not change | ~10 files, ~100 lines |
| 2 | PR 10, done | **The correlation move** (15 importers). Drop `models` and `api/correlation` from the `gateway-boundary` allow list; add an `agentkit-boundary` rule (`agentkit/**` imports only the stdlib and itself). **The gateway is cut, in place** | ~30 files, ~200 lines with step 1 |
| 3 | S20 | Move `gateway/spec` (12 files), with a facade | ~14 files |
| 4 | S20 | Move `truncation`, `providers`, `routing`, `correction`, `route`; split `truncation` from the rest if over ~95 files | ~75–80 files |
| 5 | S20 | Move the gateway root; `internal/gateway` becomes a pure facade | ~30 files |
| 6 | S20 | Migrate the **285 importers** in four batches by directory; the last one deletes the facades | 4 PRs, each under 95 files |
| 7 | S20 | Move the stage seam to `agentkit/stage` (42 importers, was 24) | ~30 files |
| 8 | S20 | Context building: `config.AgenticContextConfig` and `TopicGuardConfig` become kit `Options`; `models.Task`/`Comment` become a `TaskInput`. **New: relocate `message_editor.go` first (L1 above)** | ~15 files, ~400 lines |
| 9 | S20 | Move `agent/context` (36 importers); facade, or split by importer directory | ~65 files, or 2 PRs |
| 10 | S20 | Move `paths` (11 importers) and `agent/filecontext` (6) | ~30 files |
| 11 | S20 | Move the `ContextPack` type; optional kit record type for `aicalls` | ~20 files |
| 12+ | after the loop call | The L-steps or D-steps above | not sized |

About 13–15 PRs for the decided kit. Steps 1–2 are PR 10 and are done: the gateway's non-test code imports only the standard library, itself and `agentkit`, and the `gateway-boundary` and `agentkit-boundary` depguard rules hold it. Nothing in them depends on the loop.

## Public API

- **The seam** (`stage`): `Agent` (`Call(ctx, StageRequest) (StageResult, error)`), `StageRequest`, `StageResult`, `Outcome`, `WorkspaceAccess`, `Contract` / `NewContract[T]`, `ModelAgent` / `NewModelAgent`.
- **The LLM client**: `AIGateway`, `AIRequest`, `PromptMessage`, `Purpose`, `TaskAttrs`, and from `agentkit/llm`, `UsageDetails` and the four errors.
- **`ContextPack`**, the type only, and the context-building `Options`/`TaskInput`.
- **`correlation`**: `WithID`, `FromContext`, `Logger`.
- **Not in the API:** a read cache, a "trust your writes" prompt, the hook policies, the jail. The builtin loop as a second `stage.Agent` is exported only if it moves.

## Separate-repo criteria

Recommend a move only if at least one holds, measured over the sprint after T-045:

- a second consumer exists (another service, a CLI, an external user);
- the kit needs its own release cadence;
- fewer than ~10% of agentd PRs touch both sides of the boundary (`git log`).

| Criterion | Status |
| --- | --- |
| Second consumer | **No.** Every importer is in agentd |
| Own release cadence | **No.** Every kit change in S18 and S19 shipped with an agentd feature |
| Under ~10% cross-boundary PRs | **No.** S18: 53% (9 of 17) with the decided kit, 88% with all of `agent/`. S19 so far (PRs #173–#185, Go files, non-test, decided kit): 5 of 10; three of the five touch only 1–2 kit files. An interim number, not the measurement the ticket asks for |

**Recommendation: no separate repo, no T-046 yet.** Re-run the measurement for the sprint after T-045 with the decided-kit regex, and update this table.

### Cost of a separate repo

- **Two PRs per cross change**, about 1.5–1.9× the PRs at S18's rate, plus a kit tag and a `go.mod` bump. The agentd half cannot merge until the tag exists.
- **Version pinning:** a `require` and a tag; `go.work` or an uncommitted `replace` for local work; `GOPRIVATE` and git credentials wherever `go mod download` runs if the repo is private.
- **CI:** the kit repo duplicates `ci-go.yml`, `ci-pr-size.yml`, the golangci config (depguard, funlen 60), the `loc` and `minfunc` baselines and `tools/check*`.
- **Podman dev environment:** the `Dockerfile` runs `go mod download`, so a private kit needs a build secret, and editing both sides at once needs a sibling checkout mounted plus a `go.work` inside the container on rootless podman-compose.
- **T-046 (a module in the same repo) avoids most of this:** no credentials, one CI, one checkout. After T-045 has moved the paths it is mostly a `go.mod` and CI change.

## Recommendations for the open tickets

| Ticket | Recommendation |
| --- | --- |
| **B-057** (`delegate` does not list subagent names) | **Stays in S20 as a conditional.** Fix it (list the loaded names in the schema) only if the loop and subagents are kept; drop it if they are deleted. It is P3 and only the builtin agent is affected, so do not fix it ahead of the loop call |
| **B-089** (cut read written back loses code) | **Close the data-loss half: it is fixed** (S19 PR 02, a write to a cut file is refused). Keep the rest out of the kit and inside **T-059**: the default budget (12,000 chars cannot hold a mid-sized file), whether the cut may shorten the latest tool result, and an edit tool |
| **T-046** (`agentkit` as its own module) | **Not in S20.** Its gate is "T-045 held for a sprint with few cross-boundary PRs", which cannot be met before S21, and the interim rate is far from it. Leave it in the backlog |
| **SP-027** | Done (2026-10-31) |
