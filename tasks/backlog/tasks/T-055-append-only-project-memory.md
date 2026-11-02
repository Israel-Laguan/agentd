# T-055: Explicit Append-Only Project Memory

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | internal/memory, internal/agent/context, docs/reference.md |

## Goal

Provide a dedicated, append-only, size-bounded project memory file (e.g. `.agentd/MEMORY.md`) for high-signal lessons learned, inspired by Claudinio Code.

Rather than relying solely on automated log archival and summarization:
1. Provide an explicit mechanism for workers to record concise lessons (e.g. conventions, traps, costly discoveries) as one-line items, each capped at 200 characters.
2. Maintain strict harness-level write protection: normal file editing tools (`edit_file`) as well as shell/bash execution paths cannot modify, redirect to, or overwrite the memory file directly; only the harness `remember` flow can append entries.
3. Enforce an upper size cap (e.g., ~4,000 characters) using a bounded append-oriented log approach that preserves the append-only contract (e.g., by creating a separate compacted snapshot for retention rather than deleting/rewriting prior content).
4. Inject relevant memory entries into worker session prompts cleanly without bloating cached prompt prefixes.

## Done when

- [ ] Project memory file format and storage location established.
- [ ] Explicit append operation implemented with file-editing (`edit_file`) and shell tool write guards preventing unauthorized modification.
- [ ] Bounded append-oriented log mechanism enforces size cap without violating append-only contract.
- [ ] Unit tests verify write protection across tools, capacity bounds, and session prompt injection.

## Notes

- Claudinio reference: Saves up to five one-line lessons (200 chars each) in `.claudinio/MEMORY.md`. Only the harness writes memory (`edit_file` refuses it). Kept under ~4,000 characters by dropping oldest entries whole. Never rewritten or summarized.
