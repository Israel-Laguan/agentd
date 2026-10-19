# B-077: Host-fact guard treats tasks about agentd's own memory or disk features as host facts

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Severity | major |
| Links | internal/sandbox/host_fact_evidence.go:13 (`hostFactIntent`), B-046, B-055 (S15), found by the PR #140 e2e pass (2026-10-19) |

## Symptoms

`hostFactIntent` matches bare words: `memory`, `disk`, `storage`, `os`, `machine`, `ram`, `cpu`. A task about the memory curator or the disk watchdog is classified as a host-facts task, and its ordinary `echo … >> file` writes are refused with "host facts must come from command output, not literal text".

## Repro

1. Materialize a task titled "curator check task" whose description mentions the "memory curator".
2. The mock LLM's normal `echo … >> PLAN_RESULTS.log` command is refused with a sandbox path violation (task 4baa50bf… in the e2e pass).

## Expected

Only tasks that ask for facts about the host are held to the evidence rule.

## Actual

Any task whose text contains one of the nouns is held to it.

## Notes

- Keep the guard's purpose: host facts must come from executed commands (B-046, SP-018); B-055 already found the opposite miss (`os_info` does not match because `_` is a word character).
- Options: require host context ("this machine", "the host", "system information") next to the noun; or classify on the requested output (a host report) rather than on nouns. A design call.
- Red test first: "memory curator" and "disk watchdog" tasks are not host-fact tasks; "report the machine's RAM and CPU" still is.
