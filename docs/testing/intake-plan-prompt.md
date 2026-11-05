# Intake plan prompt: live results

S18 PR 11 measured the intake plan prompt against a real provider.

## At n = 10

Measured 2026-10-28 with the on-demand live tests in `internal/stage/intake/requires_workspace_change_live_test.go` (`go test -tags live`; the default run never calls a provider). One `intake.Plan` call per run with the PR 11 prompt, 10 runs per case, scored from the returned `DraftPlan`. **Model: `codestral-latest` (Mistral)**, the only model that finished every case. OpenRouter's free models answered 429 (quota exhausted) on most runs, or 400; a Poolside run (`poolside/laguna-s-2.1`) finished one case before its latency stalled the rest. Use a model with its own quota for these tests. The rows prove the prompt works against a real key on this model and say nothing about other models. 0 errors in every row.

The tests are behind the `live` build tag, so `make test`, `make check` and CI never run them; run them by hand when the intake prompt changes.

**`requires_workspace_change`** (right = some task flagged when the request needs a workspace change, no task flagged otherwise):

| Request | Expected | Right |
| --- | --- | --- |
| Add a `--verbose` flag to the CLI in `main.go` | flagged | 10/10 |
| Create a `README.md` that explains how to build and run the project | flagged | 10/10 |
| Fix the failing test in `parser_test.go` | flagged | 10/10 |
| what os and hardware have this machine | flagged | 10/10 |
| Write a short summary of the project; text only, do not create, modify or delete any files | not flagged | 10/10 |
| Explain the trade-offs of SQLite versus Postgres for a small app | not flagged | 10/10 |

**Vague request** (right = `clarification_question` set for a vague request, tasks planned otherwise):

| Request | Expected | Right |
| --- | --- | --- |
| make my app better | question | 10/10 |
| improve it | question | 10/10 |
| what os and hardware have this machine | plan | 10/10 |
| Add a `--verbose` flag to the CLI in `main.go` | plan | 10/10 |

Findings:

- The B-085 plan half holds on this model: both vague requests got a question every time, and the "what os and hardware" regression guard was planned every time.
- The flag is right in all six cases at 10/10. An earlier run scored the summary 9/10 and the SQLite-versus-Postgres explanation 0/10. The summary request was then reworded to "Do not create, modify, or delete any files"; the prompt itself did not change between the two runs, so the explanation case moving from 0/10 to 10/10 is not explained by a code change. Re-run before trusting the flag on read-only questions with other wording or models. The live test logs the tasks of every wrong run.
- A flagged text-only task cannot finish on text alone: it goes to a human with `UNVERIFIED_COMPLETION` unless it runs a passing command or writes a file. A false flag costs a human handoff, not a wrong COMPLETED.
- The first run's vague-request test only checked that a question came back; the n = 30 run below also checks it is specific.

## Re-run at n = 30 (2026-10-28)

Same model and tests, `AGENTD_LIVE_RUNS=30`, after two changes: the plan prompt now tells the model that a request to explain, compare or discuss is one text task with `requires_workspace_change` false and no file or command, and the vague-request test counts a question as right only when it ends in `?` and is longer than 20 characters. Errors are all 429 (quota); every run that returned an answer was right.

| Request | Expected | Right | Errors (429) |
| --- | --- | --- | --- |
| make my app better | question | 28/30 | 2 |
| improve it | question | 25/30 | 5 |
| what os and hardware have this machine | plan | 30/30 | 0 |
| Add a `--verbose` flag to the CLI in `main.go` | plan | 30/30 | 0 |
| Explain the trade-offs of SQLite versus Postgres for a small app | not flagged | 29/30 | 1 |

- The explanation case is fixed by the prompt change, not by run variance: no wrong answer in 29 answered runs, against 0/10 before.
- The specificity check removed no runs: every question codestral asked ended in `?` and was longer than 20 characters.
- The flag test's two text-only summary cases also hit 429s; their scores were not recorded, and "What does this project do?" (no "do not change files" hint) was not reported. Re-run those three before relying on the flag for plain read-only questions.
- **The prompt names the measured case.** The new rule's example is "explain the trade-offs of X versus Y", the same shape as the test request, so the 29/30 may overstate how well it generalizes. A held-out explain request with other wording would show it.

## Re-run at n = 20 with an 8 s delay (2026-10-28)

`codestral-latest`, 20 runs per case, 8 s between calls. **0 errors**: the 429s in the earlier runs were the provider's requests-per-minute limit, not the prompt. `AGENTD_LIVE_DELAY=8s` now does this in both live tests.

| Request | Expected | Right |
| --- | --- | --- |
| `--verbose` flag, README, failing test, host facts (four cases) | flagged | 20/20 each |
| Summary, "Do not create, modify, or delete any files" | not flagged | 20/20 |
| Summary, "do not change any files" (original wording) | not flagged | 20/20 |
| What does this project do? | not flagged | 18/20 |
| Explain the trade-offs of SQLite versus Postgres | not flagged | 20/20 |
| What are the pros and cons of SQLite vs Postgres? | not flagged | **16/20** |

- The explain rule fit its own example (20/20) better than a reworded request (16/20). The rule was rewritten afterwards to name kinds of request (a question, an explanation, a comparison, pros and cons, a recommendation, an opinion) instead of quoting one; **that version is not measured yet.**
- A false flag on a read-only question is cheap: the task needs one passing command or a written file, and a read such as `cat README.md` passes. The same fact makes the gate weak for a real change: any passing command counts as evidence, not only one that shows the change worked.
