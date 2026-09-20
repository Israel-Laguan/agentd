# Sprint S06 — Stabilize & Observe

| Field | Value |
| --- | --- |
| Window | 2026-09-20 |
| Goal | Harden S03-S05 tiered pipeline execution and introduce cross-system debug observability |
| Status | ready |
| Based on | [S05 retro](../S05-tiered-integration/retro/RETRO.md) |

## Goal

S03–S05 shipped the tiered execution pipeline end-to-end. The pipeline is now functional but the S05 retro surfaced open actions and a concurrency gap (SP-006) that make it fragile under load. Meanwhile, diagnosing tiered issues requires grepping raw logs. This sprint focuses on stabilization and structured observability across the pipeline.

## In scope

| ID | Type | Title | Status | Priority |
| --- | --- | --- | --- | --- |
| [US-006](stories/US-006-pipeline-debug-observability.md) | story | Pipeline debug observability | ready | P1 |
| [US-005](stories/US-005-mcp-board-export.md) | story | MCP board export wedge | ready | P2 |
| [SP-006](spikes/SP-006-atomic-tiered-origin-completion.md) | spike | Atomic tiered-origin completion | done (PR 1) | P1 |
| [B-002](bugs/B-002-context-step-committed-description.md) | bug | Context step reads unpopulated committed.Description | ready | P1 |
| [B-001](bugs/B-001-missing-web-interface.md) | bug | Missing web interface / dashboard | ready | P3 |
| [T-021](tasks/T-021-drop-task-logs.md) | task | Drop task.Logs / retire metadata API (Option A) | ready | P1 |
| [T-022](tasks/T-022-debug-on-demand-logging.md) | task | Debug-on-demand leveled logging at key boundaries | ready | P1 |

## Suggested execution

Based on a target size of 20–40 files per PR, the sprint batches into 2 pull requests:

1. **PR 1: Bug fixes & State Cleanup (~15–25 files)**
   - **Covers:** `SP-006` (Atomic completion), `B-002` (Context step bug), `B-001` (Web/health endpoints), `T-021` (Drop `task.Logs`).
   - **Why:** Highly isolated changes. Dropping `task.Logs` touches a few model definitions and escalation helpers. The bugs are tiny, localized logic fixes and basic API endpoints.

2. **PR 2: Observability + MCP Export (~25–40 files)**
   - **Covers:** `US-006` (`T-022` — debug-on-demand logging), `US-005` (MCP server).
   - **Why:** The debug logging adds `slog.Debug` calls at key subsystem boundaries (intake, router, gateway, worker loop) — silent by default, enabled via `log_level: debug` or `-v`. A correlation ID assigned at intake is carried through each stage so a stalled request is traceable end-to-end; no always-on structured telemetry or new dependencies. The MCP export introduces a new package (`internal/mcp/`). Shipping both together lets us use the new debug logs to validate the MCP integration end-to-end in a single review cycle.

## Retro

Fill [`retro/RETRO.md`](retro/RETRO.md) at close.
