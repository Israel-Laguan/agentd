package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
)

// tieredStepToolAllowlists restricts which tools each step kind may use.
// These are enforced by setting profile.AllowedTools before dispatching
// to the agentic engine, so a call outside the list is vetoed. Context and
// decision explore with read and bash; execute writes within that set; verify
// runs checks. Context and decision are not yet mounted read-only, so their
// bash can still write.
var tieredStepToolAllowlists = map[TieredStepKind][]string{
	TieredStepContext:  {"read", "bash"},
	TieredStepDecision: {"read", "bash"},
	TieredStepExecute:  {"read", "write", "bash"},
	TieredStepVerify:   {"read", "bash"},
	// Escalate needs the union of execute+verify: it both fixes the
	// conflict and re-runs checks to confirm the fix in one pass.
	TieredStepEscalate: {"read", "write", "bash"},
}

// tieredStepSystemPrompts provides per-step system prompt suffixes that
// guide the LLM to produce the expected output format.
var tieredStepSystemPrompts = map[TieredStepKind]string{
	TieredStepContext:  contextStepPrompt,
	TieredStepDecision: decisionStepPrompt,
	TieredStepExecute:  executeStepPrompt,
	TieredStepVerify:   verifyStepPrompt,
	TieredStepEscalate: escalateStepPrompt,
}

const contextStepPrompt = `
TIERED MODE: CONTEXT STEP
You are gathering context for a task. Read the workspace files relevant to
the task description. Produce a ContextPack as your ONLY output — valid JSON
matching this schema:
{
  "version": 1,
  "task_id": "<task ID>",
  "parent_task_id": "<parent task ID>",
  "created_at": "<ISO 8601>",
  "summary": "one-paragraph summary of what this task needs",
  "paths": ["file/path/1", "file/path/2"],
  "excerpts": [{"path": "file", "note": "relevant excerpt", "span": "optional line range"}],
  "commands_run": [{"cmd": "command", "outcome": "result summary"}],
  "constraints": ["must not change API surface"],
  "unknowns": ["ambiguous requirement X"],
  "budget": {"max_paths": 40, "max_chars": 48000, "path_count": <N>, "char_count": <N>}
}
Rules:
- paths must be workspace-relative and exist
- budget.path_count = len(paths), budget.char_count = total chars in summary+excerpts+commands_run+constraints+unknowns
- do NOT write any files — read-only exploration only`

const decisionStepPrompt = `
TIERED MODE: DECISION STEP
You are reading a ContextPack and deciding which files to modify and what
checks to run. Produce a Decision as your ONLY output — valid JSON:
{
  "touch_list": ["path/to/file.go", "path/to/other.go"],
  "checks": ["go test ./...", "go vet ./..."],
  "rationale": "brief explanation of the plan"
}
Rules:
- touch_list contains workspace-relative paths
- checks are shell commands to run in the verify step
- do NOT modify any files — planning only
- if the ContextPack is missing information you need (a referenced file
  doesn't exist, a constraint is unclear, critical context was never
  gathered), do NOT guess — output this instead as your ONLY output:
  {"needs_context": true, "reason": "what is missing and why"}
  This triggers a re-gather with a fresh ContextPack; it is the explicit,
  correct response to insufficient context, never a silent guess inside a
  normal Decision`

const executeStepPrompt = `
TIERED MODE: EXECUTE STEP
Apply the edits specified by the Decision step's touch_list. Read each file,
make the necessary changes, and run any build/lint commands needed.
Rules:
- only modify files in the touch_list
- prefer small, focused edits
- run tests after changes if the check commands suggest it`

const verifyStepPrompt = `
TIERED MODE: VERIFY STEP
Run the checks specified by the Decision step. For each check:
1. Execute the command
2. Classify the result as "pass", "fail", "flake", or "conflict"
3. Produce a VerifyResult as your ONLY output — valid JSON:
{
  "results": [{"check": "go test ./...", "outcome": "pass", "detail": ""}],
  "overall": "pass"
}
Rules:
- overall is "pass" if all checks pass, "fail" otherwise
- "flake" for intermittent failures, "conflict" for merge conflicts`

const escalateStepPrompt = `
TIERED MODE: ESCALATE STEP
The bounded mid-fix attempts were exhausted, or verify reported a merge
conflict. You are the strong-model escalation: read the ContextPack, the
original task, and the verify evidence in your task description, then fix
the underlying problem directly and re-run the checks yourself to confirm
the fix holds. This is the last automated attempt before the task is
handed off to a human — resolve it completely rather than partially.`

// processTieredStep dispatches a single tiered step (context, decision,
// execute, or verify) through the agentic engine with step-appropriate
// tool restrictions and system prompt injection.
func (w *Worker) processTieredStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, stepKind TieredStepKind, parentTask models.Task) {
	profileRestricted := len(profile.AllowedTools) > 0
	profile = w.applyTieredStepProfile(profile, stepKind)
	if profileRestricted && len(profile.AllowedTools) == 0 {
		w.failTieredStep(ctx, task, "no tools are permitted by the profile and tiered step allowlists")
		w.failTieredDependents(ctx, task, parentTask)
		return
	}
	profile.AgenticMode = true

	if stepKind == TieredStepContext {
		w.processTieredContextStep(ctx, task, project, profile, parentTask)
		return
	}

	if stepKind == TieredStepDecision || stepKind == TieredStepExecute || stepKind == TieredStepVerify || stepKind == TieredStepEscalate {
		var err error
		task, err = w.injectContextPack(task, parentTask, project)
		if err != nil {
			slog.Error("tiered: ContextPack injection failed; failing step",
				"task_id", task.ID, "step", stepKind, "error", err)
			w.failTieredStep(ctx, task, "ContextPack injection failed: "+err.Error())
			w.failTieredDependents(ctx, task, parentTask)
			if stepKind == TieredStepEscalate {
				if err := w.transitionTaskState(ctx, parentTask.ID, models.TaskStateFailedRequiresHuman); err != nil {
					slog.Error("tiered: failed to hand off origin after ContextPack injection failure", "task_id", parentTask.ID, "error", err)
				}
			}
			return
		}
	}

	if stepKind == TieredStepVerify {
		w.processTieredVerifyStep(ctx, task, project, profile, parentTask)
		return
	}

	if stepKind == TieredStepEscalate {
		w.processTieredEscalateStep(ctx, task, project, profile, parentTask)
		return
	}

	if stepKind == TieredStepDecision {
		w.processTieredDecisionStep(ctx, task, project, profile, parentTask)
		return
	}

	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
		if result.IsTerminalSuccess() {
			w.reconcileBlockedDependents(ctx, task.ID)
		}
	}
}

// applyTieredStepProfile clones the profile and applies step-specific
// tool allowlists and system prompt suffixes.
func (w *Worker) applyTieredStepProfile(profile models.AgentProfile, stepKind TieredStepKind) models.AgentProfile {
	p := profile
	if allowlist, ok := tieredStepToolAllowlists[stepKind]; ok {
		p.AllowedTools = intersectAllowedTools(allowlist, profile.AllowedTools)
	}
	if suffix, ok := tieredStepSystemPrompts[stepKind]; ok {
		if p.SystemPrompt.Valid {
			p.SystemPrompt.String = p.SystemPrompt.String + "\n" + suffix
		} else {
			p.SystemPrompt.String = strings.TrimSpace(suffix)
			p.SystemPrompt.Valid = true
		}
	}
	return p
}

// intersectAllowedTools keeps the step's tools that the profile also permits.
// An empty profile list means the profile is unrestricted, so the step list
// applies unchanged. A non-empty profile list that normalizes to no names
// permits nothing rather than silently becoming unrestricted.
func intersectAllowedTools(step, profile []string) []string {
	if len(profile) == 0 {
		return append([]string(nil), step...)
	}
	allowed := agenttools.ProfileAllowlist(profile)
	if allowed == nil {
		return nil
	}
	out := make([]string, 0, len(step))
	for _, name := range step {
		if agenttools.AllowsTool(allowed, name) {
			out = append(out, name)
		}
	}
	return out
}

// tieredStepProfiles is the set of AgentID values that identify tiered steps.
var tieredStepProfiles = map[string]TieredStepKind{
	tieredStepProfile[TieredStepContext]:  TieredStepContext,
	tieredStepProfile[TieredStepDecision]: TieredStepDecision,
	tieredStepProfile[TieredStepExecute]:  TieredStepExecute,
	tieredStepProfile[TieredStepVerify]:   TieredStepVerify,
	tieredStepProfile[TieredStepEscalate]: TieredStepEscalate,
}

// isTieredStep reports whether the task was created by SplitIntoTieredDAG.
func (w *Worker) isTieredStep(task models.Task) bool {
	if task.AgentID == "" {
		return false
	}
	_, ok := tieredStepProfiles[task.AgentID]
	return ok
}

// tieredStepKind returns the TieredStepKind for a tiered step task.
func (w *Worker) tieredStepKind(task models.Task) TieredStepKind {
	if kind, ok := tieredStepProfiles[task.AgentID]; ok {
		return kind
	}
	return TieredStepContext
}

// persistTieredDAG blocks the parent task and persists the tiered step
// children using SplitIntoTieredDAG output.
func (w *Worker) persistTieredDAG(ctx context.Context, parent models.Task) {
	children, _ := SplitIntoTieredDAG(parent, parent.UpdatedAt)
	dagTasks := make([]models.TieredDAGTask, len(children))
	for i, child := range children {
		var dependsOnID string
		if i > 0 {
			dependsOnID = children[i-1].ID
		}
		dagTasks[i] = models.TieredDAGTask{
			Task:        child,
			DependsOnID: dependsOnID,
		}
	}
	if _, err := w.store.PersistTieredDAG(ctx, parent.ID, parent.UpdatedAt, dagTasks); err != nil {
		slog.Error("tiered: failed to persist DAG", "task_id", parent.ID, "error", err)
		w.Emit(ctx, parent, "TIERED_DAG_PERSIST_ERROR", err.Error())
		return
	}
	w.Emit(ctx, parent, "TIERED_DAG_PERSISTED", fmt.Sprintf("created %d tiered steps", len(children)))
}
