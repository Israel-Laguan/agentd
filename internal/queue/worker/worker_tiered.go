package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

// tieredStepToolAllowlists restricts which tools each step kind may use.
// These are enforced by setting profile.AllowedTools before dispatching
// to the agentic engine. The context step is read-only; decision reads
// the pack plus workspace files; execute writes within the decision
// allowlist; verify runs checks.
var tieredStepToolAllowlists = map[TieredStepKind][]string{
	TieredStepContext:  {"read", "grep", "glob", "list"},
	TieredStepDecision: {"read", "grep", "glob", "list"},
	TieredStepExecute:  {"read", "write", "bash", "grep", "glob"},
	TieredStepVerify:   {"read", "bash", "grep", "glob"},
}

// tieredStepSystemPrompts provides per-step system prompt suffixes that
// guide the LLM to produce the expected output format.
var tieredStepSystemPrompts = map[TieredStepKind]string{
	TieredStepContext: contextStepPrompt,
	TieredStepDecision: decisionStepPrompt,
	TieredStepExecute:  executeStepPrompt,
	TieredStepVerify:   verifyStepPrompt,
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
- do NOT modify any files — planning only`

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

// processTieredStep dispatches a single tiered step (context, decision,
// execute, or verify) through the agentic engine with step-appropriate
// tool restrictions and system prompt injection.
func (w *Worker) processTieredStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, stepKind TieredStepKind, parentTask models.Task) {
	profile = w.applyTieredStepProfile(profile, stepKind)
	profile.AgenticMode = true

	if stepKind == TieredStepContext {
		w.processTieredContextStep(ctx, task, project, profile, parentTask)
		return
	}

	if stepKind == TieredStepDecision || stepKind == TieredStepExecute || stepKind == TieredStepVerify {
		w.injectContextPack(task, parentTask)
	}

	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}
}

// applyTieredStepProfile clones the profile and applies step-specific
// tool allowlists and system prompt suffixes.
func (w *Worker) applyTieredStepProfile(profile models.AgentProfile, stepKind TieredStepKind) models.AgentProfile {
	p := profile
	if allowlist, ok := tieredStepToolAllowlists[stepKind]; ok {
		p.AllowedTools = append([]string(nil), allowlist...)
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

// processTieredContextStep runs the context step: uses the agentic engine
// to gather workspace context, then intercepts the committed text to
// parse, validate, and persist the ContextPack.
func (w *Worker) processTieredContextStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		return
	}
	if !result.IsTerminalSuccess() {
		w.handleLoopResult(ctx, task, result)
		return
	}
	// The agentic engine already committed the text to the task result.
	// Read it back to extract the ContextPack.
	committed, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		slog.Error("tiered context: failed to read committed task", "task_id", task.ID, "error", err)
		return
	}
	payload := committed.Description
	if payload == "" {
		slog.Warn("tiered context: committed result is empty", "task_id", task.ID)
		return
	}
	pack, err := parseContextPack(payload)
	if err != nil {
		slog.Error("tiered context: failed to parse ContextPack", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_PARSE_ERROR", err.Error())
		return
	}
	pack.TaskID = task.ID
	pack.ParentTaskID = parentTask.ID
	pack.Budget.PathCount = len(pack.Paths)
	pack.Budget.CharCount = pack.CharCount()

	packCfg := DefaultContextPackConfig()
	if w.tieredCfg.ContextPack.MaxPaths > 0 {
		packCfg.MaxPaths = w.tieredCfg.ContextPack.MaxPaths
	}
	if w.tieredCfg.ContextPack.MaxChars > 0 {
		packCfg.MaxChars = w.tieredCfg.ContextPack.MaxChars
	}
	if _, err := pack.EnforceBudget(packCfg); err != nil {
		slog.Error("tiered context: budget enforcement failed", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_BUDGET_ERROR", err.Error())
		return
	}
	if err := pack.Validate(); err != nil {
		slog.Error("tiered context: pack validation failed", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_VALIDATION_ERROR", err.Error())
		return
	}
	if err := WriteContextPack(project.WorkspacePath, pack); err != nil {
		slog.Error("tiered context: failed to write ContextPack", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_CONTEXT_WRITE_ERROR", err.Error())
		return
	}
	w.Emit(ctx, task, "TIERED_CONTEXT_PACK_WRITTEN", PackFilePath(pack.Version))
}

// injectContextPack reads the ContextPack from the workspace and prepends
// its summary to the task description so the downstream step has context.
func (w *Worker) injectContextPack(task models.Task, parentTask models.Task) {
	packPath := PackFilePath(ContextPackVersion)
	pack, err := ReadContextPack(packPath)
	if err != nil {
		slog.Warn("tiered: failed to read ContextPack for injection",
			"task_id", task.ID, "path", packPath, "error", err)
		return
	}
	summary := fmt.Sprintf(
		"CONTEXT PACK (from %s step):\nSummary: %s\nPaths: %s\nConstraints: %s\nUnknowns: %s\n",
		TieredStepContext, pack.Summary, strings.Join(pack.Paths, ", "),
		strings.Join(pack.Constraints, "; "), strings.Join(pack.Unknowns, "; "))
	task.Description = summary + "\n\nOriginal task:\n" + task.Description
}

// parseContextPack attempts to extract a ContextPack from the LLM output.
// The output may contain JSON embedded in markdown code fences or plain text.
func parseContextPack(output string) (*ContextPack, error) {
	output = strings.TrimSpace(output)
	var cp ContextPack
	if err := json.Unmarshal([]byte(output), &cp); err == nil {
		return &cp, nil
	}
	// Try extracting from markdown code fences
	if idx := strings.Index(output, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(output[start:], "```"); end != -1 {
			jsonStr := strings.TrimSpace(output[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &cp); err == nil {
				return &cp, nil
			}
		}
	}
	if idx := strings.Index(output, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(output[start:], "```"); end != -1 {
			jsonStr := strings.TrimSpace(output[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &cp); err == nil {
				return &cp, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid ContextPack JSON found in output")
}

// tieredStepProfiles is the set of AgentID values that identify tiered steps.
var tieredStepProfiles = map[string]TieredStepKind{
	tieredStepProfile[TieredStepContext]:  TieredStepContext,
	tieredStepProfile[TieredStepDecision]: TieredStepDecision,
	tieredStepProfile[TieredStepExecute]:  TieredStepExecute,
	tieredStepProfile[TieredStepVerify]:   TieredStepVerify,
}

// isTieredStep reports whether the task was created by SplitIntoTieredDAG.
func (w *Worker) isTieredStep(task models.Task) bool {
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
