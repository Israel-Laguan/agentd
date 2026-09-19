# T-016: Tiered M3 — Worker modes (context/decision/execute/verify)

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P1 |
| Sprint | S04-tiered-pipeline |
| Estimate | XL |
| PR | **PR-B** — 20–50 files / <800 LOC |
| Links | [tiered-execution M3](../../../../docs/tiered-execution.md) |

## Goal

Implement worker dispatch modes for each step kind. Context reads and writes a ContextPack. Decision reads the pack and outputs a touch list + acceptance checks. Execute applies edits within decision bounds. Verify runs checks and classifies pass/fail/flake/conflict.

## Done when

- [ ] Context mode: reads task + workspace, emits ContextPack JSON; host orchestration persists via `WriteContextPack` API and attaches pack path to child tasks (context worker remains read-only, no write tool)
- [ ] Decision mode: reads pack, produces touch list + checks (JSON output)
- [ ] Execute mode: applies edits/commands within decision allowlist only
- [ ] Verify mode: runs decision-specified checks, classifies outcome
- [ ] Tool allowlists enforced: context = read/search/list (no write); execute = write/bash within bounds
- [ ] Broad search (repo-wide glob/grep) forbidden outside pack paths for decision/execute/verify
- [ ] Tests: each mode produces correct output, tool restriction enforced, pack injection works
- [ ] No prod changes to existing legacy/agentic paths

## Notes

- Worker modes are dispatched by step kind on the task's agent profile
- ContextPack is the sealed handoff — decision/execute/verify inject it via system prompt; persistence is host-side via `WriteContextPack` (see T-008), not a context-model write tool
- Tool allowlists can reuse the existing `ToolManifest` / `AllowedTools` mechanism; context allowlist is strictly read/search/list
- Agentic mode allowed on execute and escalate only (per spec)
