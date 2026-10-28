package intake

import (
	"context"
	"encoding/json"
	"fmt"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// hostFactPlanningRule keeps plans about the machine itself executable: tasks run
// as shell commands in a jailed workspace, so facts must come from commands.
const hostFactPlanningRule = "\nTasks run as shell commands inside the project workspace and cannot read files outside it, so never name /etc, /proc or /sys paths in task descriptions. " +
	"When the request asks about this machine's OS or hardware, gather the facts with commands such as uname -a, hostnamectl, lscpu, free -h, lsblk and df -h, " +
	"saving each output to a file, and finish with a task titled \"" + models.HostReportTaskTitle + "\" that builds report.md from those files; never state facts you did not collect. " +
	"Name files with bare relative file names such as cpu_info.txt (never a /workspace or workspace/ prefix), and give every collecting task a ref_id and the report task a depends_on listing all of them, because tasks without depends_on run in parallel."

// workspaceChangePlanningRule asks for the flag the worker's evidence gate reads
// (PR 11): a task that changes the workspace cannot finish on text alone.
const workspaceChangePlanningRule = "\nSet requires_workspace_change to true on every task that must create or change a file, or run a command, in the project workspace. " +
	"Set it to false, or leave it out, on a task that only reasons or writes text, such as a summary or a review."

// vagueRequestPlanningRule makes the plan step ask instead of planning discovery
// for a request that names no target or outcome (B-085). A request that asks
// for a concrete read-only answer, such as facts about this machine, is not vague.
const vagueRequestPlanningRule = "\nIf the request names no target and no outcome, such as \"make my app better\" or \"improve it\", do not plan tasks that only explore the project to find out what to do. " +
	"Set clarification_question to one specific question that asks what should change and how the user will know it is done, and leave tasks empty. " +
	"A request for a concrete answer, such as facts about this machine, is not vague: plan it."

func phasePlanningPrompt(maxTasksPerPhase int) string {
	base := "Create an agentd DraftPlan. Output ONLY valid JSON.\n" + models.DraftPlan{}.SchemaHint() + hostFactPlanningRule + workspaceChangePlanningRule + vagueRequestPlanningRule
	if maxTasksPerPhase <= 0 {
		return base
	}
	return base + fmt.Sprintf(
		"\nEmit at most %d tasks. If more work remains, make the last task titled %q with a description summarizing remaining work.",
		maxTasksPerPhase,
		"Plan Phase 2",
	)
}

// Plan asks agent for a DraftPlan for userIntent, tagged with purpose, and
// trims it to maxTasksPerPhase. Callers that create tasks from a purpose other
// than a chat intake pass that purpose explicitly.
func Plan(ctx context.Context, agent stage.Agent, purpose spec.Purpose, maxTasksPerPhase int, userIntent string) (*models.DraftPlan, error) {
	res, err := agent.Call(ctx, stage.StageRequest{
		Stage:   stage.StageIntake,
		Purpose: purpose,
		Context: []spec.PromptMessage{
			{Role: "system", Content: phasePlanningPrompt(maxTasksPerPhase)},
			{Role: "user", Content: userIntent},
		},
		Contract:    stage.NewContract[models.DraftPlan](),
		Temperature: 0.2,
	})
	if err != nil {
		return nil, err
	}
	plan, err := decode[models.DraftPlan](res)
	if err != nil {
		return nil, err
	}
	trimmed := correction.EnforcePhaseCap(*plan, maxTasksPerPhase)
	return &trimmed, nil
}

// decode reads the validated output of a stage into T.
func decode[T any](res stage.StageResult) (*T, error) {
	var v T
	if err := json.Unmarshal(res.Output, &v); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidJSONResponse, err)
	}
	return &v, nil
}
