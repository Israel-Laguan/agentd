---
name: pr-review-replies
description: >-
  Triage automated PR review comments (gemini-code-assist, CodeRabbit, Bugbot),
  verify each item against the codebase, and post a targeted reply on every
  comment thread. Use when the user asks to respond to PR review feedback,
  double-check if review comments are solved, or reply to bot review comments.
---

# PR Review Replies

Triage each review comment individually. Post one targeted reply per comment thread — never a single general PR comment.

## Workflow

```
Task Progress:
- [ ] Find the PR for the current branch
- [ ] Fetch review comments (filter by bot if requested)
- [ ] For each comment: verify status in codebase
- [ ] Post a targeted reply on each thread
- [ ] Summarize outcomes for the user
```

### Step 1: Find the PR

```bash
gh pr list --head "$(git branch --show-current)" --json number,title,url
```

If no PR on current branch, ask the user for the PR number or URL.

### Step 2: Fetch review comments

List inline review comments (file/line threads):

```bash
gh api repos/{owner}/{repo}/pulls/{number}/comments --paginate
```

Filter by bot when the user names one (e.g. gemini-code-assist):

```bash
gh api repos/{owner}/{repo}/pulls/{number}/comments --paginate \
  | jq -r '.[] | select(.user.login | test("gemini|code-assist"; "i")) | {id, path, line, body: (.body | split("\n")[0:3] | join(" "))}'
```

Record each comment's `id` — required for replies.

### Step 3: Verify each comment

For every comment, read the referenced file/lines and check git history:

```bash
git log --oneline -10
git show {commit} -- {file}
```

Classify each comment:

| Status | When | Action |
|--------|------|--------|
| **Solved** | Fix already in branch (cite commit) | Reply only |
| **Needs action** | Valid gap not yet fixed | Implement fix, then reply |
| **No action needed** | Suggestion rejected or out of scope | Reply with brief rationale |

When the reviewer suggested one approach but the branch uses a better equivalent (e.g. returned errors vs `slog.Error`), classify as **Solved** and explain the chosen approach in the reply.

### Step 4: Post targeted replies

Use the GitHub API with JSON — `in_reply_to` must be a **number**, not a string:

```bash
gh api repos/{owner}/{repo}/pulls/{number}/comments -X POST --input - <<'EOF'
{
  "body": "Addressed in abc1234. Brief explanation of what changed and why.",
  "in_reply_to": 3295053631
}
EOF
```

Do **not** use `-f in_reply_to=...` (GitHub treats it as a string and returns 422).

Post one reply per original comment. Run replies in parallel when independent.

### Step 5: Report to user

Summarize with a table: comment topic, status, reply link. No need to repeat full reply text.

## Reply templates

**Solved (already fixed):**

> Addressed in {short-commit}. {One sentence: what was done and where.}

**Solved (different but equivalent approach):**

> Addressed in {short-commit}. {What was done instead of the suggestion, and why — e.g. fail-fast errors propagate through config load rather than log-and-continue.}

**No action needed:**

> No action needed. {One sentence rationale.}

**Fixed during this session:**

> Addressed in {short-commit}. {One sentence describing the fix.}

Keep replies concise. Reference commit shorthands (7 chars) and file paths when helpful.

## Rules

- Reply on **each comment thread**, not as a single PR-level comment.
- Verify against **current branch code**, not only the diff the bot reviewed.
- If a comment asks for tests and tests exist, mention them in the reply.
- Do not edit plan files or create commits unless the user asks.
- Use `gh` for all GitHub operations; request `full_network` permission when needed.

## Example

Three gemini-code-assist comments on PR #63:

1. Define `chatToolsChecker` interface → Solved in `10f197b` → reply on thread 3295053631
2. Add `mapstructure` tags → Solved in `10f197b`, tests cover it → reply on thread 3295053643
3. Log unmarshal errors → Solved with fail-fast errors → reply on thread 3295053647

Each gets its own reply; user gets a summary table with discussion links.
