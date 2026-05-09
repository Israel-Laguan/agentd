package worker

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type TaskRunner struct {
	gateway gateway.AIGateway
	store   models.KanbanStore
	emitter models.EventSink
	ws      sandbox.WorkspaceManager
}

func NewTaskRunner(
	gw gateway.AIGateway,
	store models.KanbanStore,
	emitter models.EventSink,
	ws sandbox.WorkspaceManager,
) *TaskRunner {
	return &TaskRunner{gateway: gw, store: store, emitter: emitter, ws: ws}
}

func (r *TaskRunner) Suggest(ctx context.Context, taskID string) (string, error) {
	task, project, profile, err := r.loadTaskContext(ctx, taskID)
	if err != nil {
		return "", err
	}
	cmd, err := r.commandSuggestion(ctx, task, profile)
	if err != nil {
		return "", err
	}
	workspace := r.ws.ProjectDir(project.ID)
	suggestion := fmt.Sprintf("cd %s && %s", filepath.Clean(workspace), cmd.Command)
	return suggestion, r.emitSuggestion(ctx, task, suggestion)
}

func (r *TaskRunner) loadTaskContext(ctx context.Context, taskID string) (*models.Task, *models.Project, *models.AgentProfile, error) {
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, nil, nil, err
	}
	project, err := r.store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return nil, nil, nil, err
	}
	profile, err := r.store.GetAgentProfile(ctx, task.AgentID)
	return task, project, profile, err
}

func (r *TaskRunner) commandSuggestion(
	ctx context.Context,
	task *models.Task,
	profile *models.AgentProfile,
) (suggestedCommand, error) {
	req := gateway.AIRequest{
		Messages:    suggestionMessages(task, profile),
		Temperature: profile.Temperature,
		JSONMode:    true,
		AgentID:     task.AgentID,
		Role:        gateway.RoleWorker,
		TaskID:      task.ID,
	}
	return gateway.GenerateJSON[suggestedCommand](ctx, r.gateway, req)
}

func suggestionMessages(task *models.Task, profile *models.AgentProfile) []gateway.PromptMessage {
	system := "Return JSON with a single shell command: {\"command\":\"...\"}."
	if profile.SystemPrompt.Valid {
		system = profile.SystemPrompt.String
	}
	return []gateway.PromptMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: task.Description},
	}
}

func (r *TaskRunner) emitSuggestion(ctx context.Context, task *models.Task, suggestion string) error {
	return r.emitter.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "SUGGESTION",
		Payload:   suggestion,
	})
}

type suggestedCommand struct {
	Command string `json:"command"`
}
