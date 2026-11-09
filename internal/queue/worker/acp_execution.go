package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"agentd/internal/agent/acp"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// acpExecution is what an acp agent reports for exec.turn.
type acpExecution struct {
	Summary string `json:"summary"`
}

// SchemaHint describes the acp execution contract's expected output.
func (acpExecution) SchemaHint() string {
	const intro = "A JSON object whose summary is committed as the task's result: "
	const shape = `{"summary": "what you changed and how you checked it"}`
	return intro + shape
}

// Validate checks the acp execution's reported summary.
func (e acpExecution) Validate() error {
	if strings.TrimSpace(e.Summary) == "" {
		return errors.New("summary is required")
	}
	return nil
}

// runACPExecution runs the task on the acp agent its exec.turn route names and
// applies the outcome as the builtin loop's is applied: a completion is
// committed, a question goes to a human, an unverified completion fails to a
// human (B-082), and any other error takes the gateway error path (breaker,
// defer or retry).
func (w *Worker) runACPExecution(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, resolved executionResolution) {
	audit := w.beginTaskAudit(task, project, route.KindACP)
	succeeded := false
	ctx, cancel, done := w.registerRunCancel(ctx, task.ID)
	defer cancel()
	defer done()
	defer func() {
		audit.failed = !succeeded && !audit.suspended
		w.finishTaskAudit(task, project, route.KindACP, audit)
	}()
	access := stage.AccessWrite
	if profile.WorkspaceReadonly {
		if task.RequiresWorkspaceChange {
			w.failTerminal(ctx, task, stage.ErrUnverifiedCompletion, models.TaskStateFailedRequiresHuman)
			return
		}
		access = stage.AccessReadOnly
	}
	ctx = gateway.WithTaskID(ctx, task.ID)
	taskHooks, _ := w.MountAgenticHooks(project, profile)
	if err := w.RunSessionStart(ctx, task, project, taskHooks); err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	clarified, blocked, err := w.RunPreTaskElicitation(ctx, task, project)
	if err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	if blocked {
		audit.suspended = true
		return
	}
	task = clarified
	res, err := w.callACP(ctx, task, project, profile, resolved.route, resolved.agent, w.acpExecutionRequest(ctx, task, project, profile, access), taskHooks, true)
	w.RecordTaskTokenUsage(ctx, task, res.TokenUsage, res.UsageDetails)
	succeeded = w.applyACPExecutionResult(ctx, task, project, profile, res, err, audit)
}

func (w *Worker) applyACPExecutionResult(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, res stage.StageResult, err error, audit *taskAudit) bool {
	switch {
	case errors.Is(err, acp.ErrSuspended):
		// An approval gate blocked the task on a human; it runs again once answered.
		audit.suspended = true
		return false
	case errors.Is(err, stage.ErrUnverifiedCompletion):
		w.failTerminal(ctx, task, err, models.TaskStateFailedRequiresHuman)
	case err != nil:
		w.HandleGatewayError(ctx, task, err)
	case res.Outcome == stage.OutcomeNeedsHuman:
		if err := handOffNeedsHuman(ctx, w.AskHuman, task, gateway.PurposeExecTurn, res.Output); !errors.Is(err, stage.ErrNeedsHuman) {
			w.FailHard(ctx, task, err)
		} else {
			audit.suspended = true
		}
	default:
		var out acpExecution
		_ = json.Unmarshal(res.Output, &out) // the contract has validated it
		return w.commitExecution(ctx, task, project, profile, out.Summary)
	}
	return false
}

func (w *Worker) acpExecutionRequest(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, access stage.WorkspaceAccess) stage.StageRequest {
	return stage.StageRequest{
		Stage:    stage.StageExecution,
		Purpose:  gateway.PurposeExecTurn,
		Context:  w.buildStageContext(ctx, task, project, profile),
		Contract: stage.NewContract[acpExecution](),
		Access:   access,
		AgentID:  task.AgentID,
		Attrs:    gateway.AttrsForTask(task.RequiresWorkspaceChange),
	}
}
