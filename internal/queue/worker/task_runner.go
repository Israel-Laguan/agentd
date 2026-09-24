package worker

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type TaskRunner struct {
	gateway      gateway.AIGateway
	store        models.KanbanStore
	emitter      models.EventSink
	ws           sandbox.WorkspaceManager
	projectsRoot string
}

func NewTaskRunner(
	gw gateway.AIGateway,
	store models.KanbanStore,
	emitter models.EventSink,
	ws sandbox.WorkspaceManager,
	projectsRoot ...string,
) *TaskRunner {
	root := ""
	if len(projectsRoot) > 0 {
		root = projectsRoot[0]
	}
	return &TaskRunner{gateway: gw, store: store, emitter: emitter, ws: ws, projectsRoot: root}
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
	if project.ID == "_system" || project.WorkspacePath == "_system" {
		return "", fmt.Errorf("%w: system project is not an executable workspace", models.ErrSandboxViolation)
	}
	if r.projectsRoot == "" {
		return "", fmt.Errorf("workspace root is not configured")
	}
	if !filepath.IsAbs(project.WorkspacePath) || filepath.Clean(project.WorkspacePath) != project.WorkspacePath {
		return "", fmt.Errorf("%w: project workspace must be a clean absolute path: %s", models.ErrSandboxViolation, project.WorkspacePath)
	}
	workspace, err := sandbox.JailPath(r.projectsRoot, project.WorkspacePath)
	if err != nil {
		return "", fmt.Errorf("validate project workspace: %w", err)
	}
	if workspace == filepath.Clean(r.projectsRoot) {
		return "", fmt.Errorf("%w: refusing to suggest commands at workspace root", models.ErrSandboxViolation)
	}
	suggestion := fmt.Sprintf("cd %s && %s", shellQuote(workspace), cmd.Command)
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
