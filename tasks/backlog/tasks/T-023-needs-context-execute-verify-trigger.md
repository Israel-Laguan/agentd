# T-023: NEEDS_CONTEXT trigger from execute/verify steps

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Parent | none |
| Estimate | L |
| Links | [S05 retro](../../sprints/S05-tiered-integration/retro/RETRO.md), `docs/tiered-execution.md`, `internal/queue/worker/escalation.go` |

## Goal

T-017 originally specified NEEDS_CONTEXT could be triggered "if decision/execute/verify step determines" more context is needed. S05 (T-020) shipped only the decision-step trigger (an explicit `{"needs_context": true, ...}` sentinel in the decision prompt) because committing today happens unconditionally inside `engine.Process` before tiered-specific code regains control — there's no hook to intercept a result before commit from execute/verify.

Extending the trigger to execute/verify needs an agentic-engine change (pre-commit interception), which is materially bigger than the S05 ticket that surfaced the gap. It was flagged as an unassigned retro action and never turned into its own ticket.

## Done when

- [ ] Decide whether pre-commit interception in `engine.Process` is worth adding, or whether decision-only NEEDS_CONTEXT is the accepted permanent scope (update `docs/tiered-execution.md` either way)
- [ ] If proceeding: engine supports intercepting a step's result before commit for tiered steps
- [ ] Execute/verify steps can signal NEEDS_CONTEXT the same way decision does today
- [ ] Existing decision-step NEEDS_CONTEXT path and its tests are unaffected

## Notes

- Explicitly scope this before starting — the S05 retro called it out as "bigger than this sprint's other work."
