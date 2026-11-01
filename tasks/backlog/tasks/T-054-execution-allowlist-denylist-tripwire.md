# T-054: Execution Allowlist and Denylist Tripwire Hardening

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | internal/sandbox, internal/queue/worker/worker_permission.go, GUARDRAILS.md |

## Goal

Harden sandboxed command execution with a strict dual-tier security model: an allowlist for fast-path safe commands and a substring tripwire denylist for destructive commands, inspired by Claudinio Code.

Key architectural boundaries:
1. Safe commands (read-only inspection such as `ls`, `cat`, `head`, `tail`, `wc`, standard test runners/builds without side-effect piping or destructive redirections) execute without extra friction.
2. Hard denylist tripwire: low-level normalized substring inspection (stripping quotes, escapes, backticks, collapsing whitespace) catching destructive commands (`rm -rf /`, `mkfs`, `dd if=`, device redirection, dangerous permissions) and refusing them outright.
3. Neither hooks nor relaxed execution policies can override or relax the hard denylist tripwire.

## Done when

- [ ] Command evaluator enforces fast-path allowlist and tripwire denylist with string normalization.
- [ ] Known destructive patterns are blocked unconditionally regardless of flags or execution modes.
- [ ] Security rules and tripwires are documented in `GUARDRAILS.md`.
- [ ] Unit tests test various bypass attempts (quotes, backticks, whitespaces, chaining) against the denylist tripwire.

## Notes

- Claudinio reference: Allowlisted inspection/test commands run unasked; denylist is a substring tripwire where normalized text matching destructive patterns is refused outright and cannot be relaxed by hooks or YOLO modes.
