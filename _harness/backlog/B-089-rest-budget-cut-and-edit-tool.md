# B-089 (rest): agentic budget, the cut of the latest tool result, an edit tool

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog (planned in agentd, never executed; documented in the harness-extraction plan on 2026-11-05  — the builtin loop still runs in agentd, its removal is a pending T-071/T-072 step) |
| Priority | P2 |
| Sprint | — (agentd: T-059 in S17, never scheduled) |
| Links | agentd S19 B-089 (data-loss half fixed in agentd #176), SP-027, `queue/worker/agentic/cut_log.go`, `agent/tools/cut_reads.go`, `agent/context` |

## Symptoms

On the default config the agentic budget inherits `gateway.truncator.max_input_chars` (12,000 chars), so a file over about 9,700 chars reaches the model cut short. SP-027 (2026-10-31, 12 tasks on `codestral-latest`) lost code in 5 of 12 workspaces when the model wrote the cut copy back. agentd #176 refuses a whole-file `write` to a file whose latest read was cut, which stops the data loss. The causes are still there.

## What is left

- **Budget:** derive the agentic budget from the routed model's context window instead of the 12,000-char gateway default.
- **Cut order:** never cut the latest tool result; cut older exchanges first. Today the turn after a read is cut by `agentic_head` (26 of 73 turns in SP-027), which drops the read and the model's own write.
- **Edit tool:** replace a string or a line range, so a change does not need the whole file. It also cuts the cost of repeated reads (17.5% of `exec.turn` input in SP-027).
- **Re-measure** repeated reads after the three changes above.

## Notes

- A prompt rule alone does not fix it: the cut copy carried the truncation marker and the model still wrote it back.
