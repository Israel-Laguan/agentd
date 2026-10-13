# SP-018: Should workers be able to run read-only host introspection (OS / hardware)?

| Field | Value |
| --- | --- |
| Type | spike |
| Status | backlog |
| Priority | P2 |
| Sprint | backlog |
| Time box | 1 day |
| Links | B-032, internal/sandbox/sanitize.go, GUARDRAILS.md |

## Question

"What OS and hardware does this machine have?" is a natural first task, yet the sandbox forbids touching anything outside the project workspace. Do we want a narrow, read-only allowlist (`/etc/os-release`, `/proc/cpuinfo`, `/proc/meminfo`, `/sys/class/dmi`, `/dev/null`) or an `inspect` command set (`uname`, `lscpu`, `free`, `lsblk`), and what does that do to the "Signs" safety model?

Related observation: with a weaker model, the planner/worker tends to answer from imagination. Example: the first run's raw planner output claimed "Ubuntu 22.04, i7-9700K, 32 GB", while the real host is Debian 13 (trixie), Linux 7.2.9-liquorix, Intel Core Ultra 5 226V. Should a task whose success criterion is host facts require evidence from a command run, with the output attached?

## Output

- [ ] Go/no-go on a read-only allowlist, with the threat analysis (symlinks, `/proc/*/environ`, `/proc/self/root`).
- [ ] If go: design note on where the allowlist lives (config vs code) and how it's logged.
- [ ] Decision on whether success criteria that claim host facts need captured command output.

## Out of scope

- Network or write access outside the workspace.

## Notes

- Found during the first manual end-to-end test, 2026-10-13.
