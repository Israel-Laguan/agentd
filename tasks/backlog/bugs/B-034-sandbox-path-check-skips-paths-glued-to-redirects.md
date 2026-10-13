# B-034: Sandbox path check skips absolute paths glued to a redirect operator (`>/etc/x`, `</etc/passwd`)

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Severity | major |
| Links | internal/sandbox/sanitize.go (`validateAbsolutePathTokens`), B-032, SP-018, GUARDRAILS.md |

## Symptoms

`validateAbsolutePathTokens` splits a command on whitespace and `;|&()` and only checks tokens that are themselves absolute. A path attached to a redirect operator is one token that starts with `>` or `<` (or `2>`), so it is never checked and the command is allowed to run.

## Repro

Throwaway probe test in `internal/sandbox` (not committed), workspace `/workspace`:

| Command | `validateCommandPaths` result |
| --- | --- |
| `cat /etc/passwd` | rejected: absolute path escapes workspace |
| `echo hi >/etc/agentd-probe` | **nil (allowed)** |
| `cat </etc/passwd` | **nil (allowed)** |
| `echo hi 2>/etc/x` | **nil (allowed)** |

The same applies to any form where the path follows `>`, `>>`, `<` or `N>` with no space, and to `--opt=/abs/path` style arguments (not probed).

## Expected

Any absolute path outside the workspace in the command text is refused, whether or not it is attached to an operator or an option.

## Actual

Only whitespace-separated absolute tokens are checked, so the redirect forms above pass the sandbox check. Actual write/read success then depends on host file permissions, not on the sandbox.

## Notes

- Found while fixing B-032, which wrongly assumed `2>/dev/null` was rejected; it was `> /dev/null` (with a space) that was. B-032 now allows the literal `/dev/null` token only.
- Fix direction: strip leading redirect operators and `key=` prefixes from each token before the absolute-path test, or tokenize with a real shell lexer. Needs a table-driven test covering the cases above. Any allowlist change belongs to SP-018.
- Not fixed in B-032 on purpose: it is a security-relevant behavior change that deserves its own PR and review.
