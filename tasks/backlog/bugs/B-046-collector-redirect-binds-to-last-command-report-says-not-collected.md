# B-046: `a && b > file` leaves the file without `a`'s output, and the report silently says "not collected"

| Field | Value |
| --- | --- |
| Type | bug |
| Status | review (branch fix/b-046-collector-redirect-detection, not pushed) |
| Priority | P2 |
| Sprint | backlog (found in S14 live run 2026-10-15) |
| Severity | minor |
| Links | B-045, B-039, internal/queue/hostreport |

## Symptoms

Live run on `scratch/s14-live`: the disk collector ran `lsblk && df -h > disk_info.txt 2>&1`. The redirect binds only to `df -h`, so `disk_info.txt` held `df` output and no `lsblk` table. The code-rendered `report.md` then showed `Disks: not collected` while every task was COMPLETED.

## Expected

A collecting task's output file holds the output of every command the task names, and a report with a missing core fact does not pass silently.

## Actual

The worker accepts exit 0 and B-039's post-condition only checks the file exists and is non-empty.

## Notes

- Fixed 2026-10-16 on branch fix/b-046-collector-redirect-detection (not
  pushed), three parts each with tests: (a) admission refuses chained
  `a && b > file` on host-fact tasks with grouping retry feedback
  (internal/sandbox/host_fact_evidence.go); (b) a partial collection fails
  the report task with retry feedback naming missing facts and files read
  (*MissingFactsError; no-facts-at-all still falls back to the model path);
  (c) disks fall back to `df -h` /dev rows when no lsblk table exists.
  Machine ID / Boot ID / hostname stay out of reports; symlinks unfollowed.

- Options: worker/planner guidance to group (`{ a; b; } > file`) or one command per file; a renderer fallback that reads disk sizes from `df`; making the report task fail (retry feedback) when a core fact is `not collected`.
- Not a B-045 regression: `REPORT_RENDERED` fired and the other facts matched.
