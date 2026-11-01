# T-051: Adaptive Context Shedding and Compaction Notes

| Field | Value |
| --- | --- |
| Type | task |
| Status | backlog |
| Priority | P1 |
| Sprint | backlog |
| Parent | none |
| Estimate | M |
| Links | internal/agent/context/context_manager.go, docs/why-agentd.md |

## Goal

Implement adaptive context shedding before LLM turn summarization and generate structured compaction notes, inspired by Claudinio Code's context hygiene mechanisms.

Instead of jumping straight to LLM turn summarization or brutal one-sided context truncation:
1. Prune redundant older copies of tool results (e.g., an earlier read of a file when a newer read exists, or stale task listings).
2. Retain both head and tail of long tool outputs (command executions, test runs) so the initial command invocation and the final verdict/exit status are preserved.
3. When summarization/compaction occurs, produce a structured markdown compaction note (capturing golden goals, decisions, files touched, exact error strings, remaining work, and user constraints verbatim) and append to a durable notes log.

## Done when

- [ ] `ContextManager` sheds duplicate or obsolete read results before triggering LLM rolling summarization.
- [ ] Tool output truncation strategy supports head + tail retention with an indicator of omitted byte/line count.
- [ ] Compaction records a structured summary note appended to project task notes.
- [ ] Unit tests cover duplicate shedding, head+tail truncation, and compaction note generation.

## Notes

- Claudinio reference: Context is shed before it is summarized (older reads of same file, older task lists). A long tool output keeps its beginning and end where test/build verdicts sit. Compaction notes preserve user constraints word-for-word.
