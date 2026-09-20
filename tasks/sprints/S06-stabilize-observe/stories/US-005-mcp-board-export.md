# US-005: MCP board export wedge

| Field | Value |
| --- | --- |
| Type | user-story |
| Status | ready |
| Priority | P2 |
| Sprint | S06-stabilize-observe |
| Persona | adopter |
| Links | [product-plan Phase 3](../../../docs/product-plan.md) |

## Story

As an **adopter already using Claude Code/Cline**, I want **agentd’s board exposed over MCP**, so that **those tools can work as workers behind our control plane.**

## Acceptance criteria

- [ ] Spec for read path (+ gated write)
- [ ] Prototype MCP server with working read path; either read-only, or writes enforced through the agentd gate and approval boundary
- [ ] Docs: agentd owns board; external agent is worker

## Notes

-
