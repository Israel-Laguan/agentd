# T-021: Drop task.Logs / retire metadata API

| Field | Value |
| --- | --- |
| Type | task |
| Status | review |
| Priority | P1 |
| Sprint | S06-stabilize-observe |
| Links | [S05 retro](../../S05-tiered-integration/retro/RETRO.md) |

## Goal

Remove the `task.Logs` field and the `getMetadata`/`setMetadata` API. It has no backing DB column and doesn't survive reloads. The system now uses durable counts (SPAWNED_BY) anyway.

## Acceptance criteria

- `Logs` field removed from `Task` struct
- `getMetadata` and `setMetadata` removed from `escalation.go`
- Callers updated to use the durable child-count approach directly
