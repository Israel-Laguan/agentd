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
2. Advisory denylist tripwire: low-level normalized substring inspection (stripping quotes, escapes, backticks, collapsing whitespace) catching obvious destructive commands (`rm -rf /`, `mkfs`, `dd if=`, device redirection, dangerous permissions) and refusing them outright.
3. Hard boundary clarification: string tripwires are defense-in-depth and advisory only (since shell variable expansion, aliases, and command substitutions can evade text normalization); true isolation and security enforcement must rely on sandbox boundaries (e.g. bubblewrap jail) or parsed restricted command execution.
4. Neither hooks nor relaxed execution policies can override or relax the tripwire denylist.

## Done when

- [ ] Command evaluator enforces fast-path allowlist and advisory tripwire denylist with string normalization.
- [ ] Known destructive patterns are blocked by the tripwire regardless of flags or execution modes.
- [ ] Sandbox isolation is reinforced as the hard security boundary for evasion attacks (e.g., variable expansion, subshells).
- [ ] Security rules and tripwires are documented in `GUARDRAILS.md`.
- [ ] Unit tests verify denylist tripwire behavior, including bypass attempts and expansion boundaries.

## Notes

- Claudinio reference: Allowlisted inspection/test commands run unasked; denylist is a substring tripwire where normalized text matching destructive patterns is refused outright and cannot be relaxed by hooks or YOLO modes.
