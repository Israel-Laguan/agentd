package worker

import (
	"context"
	"fmt"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func taskIntent(task models.Task) string {
	return task.Title + " " + task.Description
}

func (w *Worker) prependMemoryLessons(ctx context.Context, intent string, projectID string, messages []gateway.PromptMessage) []gateway.PromptMessage {
	if w.retriever == nil {
		return messages
	}
	recalled := w.retriever.Recall(ctx, intent, projectID, "")
	if lessons := memoryFormatLessons(recalled); lessons != "" {
		return append([]gateway.PromptMessage{{Role: "system", Content: lessons}}, messages...)
	}
	return messages
}

func (w *Worker) seedMessages(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	messages := w.legacySeedMessages(task, project, profile)
	intent := taskIntent(task)
	return w.prependMemoryLessons(ctx, intent, task.ProjectID, messages)
}

func (w *Worker) legacySeedMessages(task models.Task, _ models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	return workerMessages(task, profile)
}

func agenticToolUseSystemText(goal ...*AgentGoal) string {
	text := `You are an autonomous agent that can execute shell commands, read files, and write files to complete tasks.
When you need to execute a command, use the bash tool.
When you need to read a file, use the read tool.
When you need to create or modify a file, use the write tool.
Return your response as plain text when the task is complete, or use tools to continue working.

` + externalContentInstruction
	if len(goal) == 0 || goal[0] == nil || len(goal[0].SuccessCriteria) == 0 {
		return text
	}
	criteria := "\nTask success criteria:\n"
	for _, c := range goal[0].SuccessCriteria {
		criteria += fmt.Sprintf("- %s\n", c)
	}
	return text + criteria + `
When a success criterion becomes complete, include a line exactly like [COMPLETED] criterion text.
When a success criterion is blocked, include a line exactly like [BLOCKED] criterion text.
Use the exact criterion text from the task success criteria.`
}

// enrichBuilderUserPreferences loads user-level instructions into the system prompt builder.
func (w *Worker) enrichBuilderUserPreferences(builder *SystemPromptBuilder) {
	if w.instructionLoader == nil {
		return
	}
	prefs, err := w.instructionLoader.LoadUserPreferences()
	if err != nil {
		slog.Warn("failed to load user preferences", "error", err)
		return
	}
	if prefs != nil {
		builder.WithUserPreferences(prefs)
	}
}

func (w *Worker) enrichBuilderProjectInstructions(builder *SystemPromptBuilder, project models.Project, profile models.AgentProfile) {
	if w.instructionLoader == nil || project.WorkspacePath == "" {
		return
	}
	instructions, err := w.instructionLoader.LoadProjectInstructions(project.WorkspacePath, profile.InstructionsPath)
	if err != nil {
		slog.Warn("failed to load project instructions", "workspace", project.WorkspacePath, "error", err)
		return
	}
	if instructions != nil {
		builder.WithProject(instructions)
	}
}

func (w *Worker) enrichBuilderMatchedSkills(builder *SystemPromptBuilder, task models.Task, project models.Project) {
	if w.skillLoader == nil || w.skillRouter == nil {
		return
	}
	skills, err := w.skillLoader.LoadAll(project.WorkspacePath)
	if err != nil {
		slog.Warn("failed to load skills", "workspace", project.WorkspacePath, "error", err)
		return
	}
	if len(skills) == 0 {
		return
	}
	intent := taskIntent(task)
	matched := w.skillRouter.Match(intent, skills)
	for _, sk := range matched {
		builder.AddSkillBlock(FormatSkillBlock(sk))
	}
	if len(matched) > 0 {
		slog.Debug("injected matched skills into system prompt", "task_id", task.ID, "count", len(matched))
	}
}

func (w *Worker) buildSystemPromptContent(task models.Task, project models.Project, profile models.AgentProfile) string {
	var goal *AgentGoal
	if g := GoalFromTask(task); g != nil {
		goal = g
	}
	builder := NewSystemPromptBuilder().
		WithGlobal(agenticToolUseSystemText(goal))
	w.enrichBuilderUserPreferences(builder)
	w.enrichBuilderProjectInstructions(builder, project, profile)
	w.enrichBuilderMatchedSkills(builder, task, project)
	if profile.SystemPrompt.Valid {
		builder.WithTask(profile.SystemPrompt.String)
	}
	return builder.Build()
}

func defaultTaskUserContent(task models.Task) string {
	return fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
}

func (w *Worker) buildPromptMessages(task models.Task, project models.Project, profile models.AgentProfile) (system, user string) {
	systemPrefix := w.buildSystemPromptContent(task, project, profile)
	if name, slots, ok := w.promptTemplateForTask(task, profile); ok {
		rendered, err := w.promptLibrary.Render(name, RenderSession{SystemPrefix: systemPrefix}, slots)
		if err != nil {
			slog.Warn("prompt template render failed, using default messages",
				"template", name, "task_id", task.ID, "error", err)
		} else {
			return rendered.System, rendered.User
		}
	}
	return systemPrefix, defaultTaskUserContent(task)
}

// assembleAgenticSystemPrompt builds the full layered system prompt for agentic
// mode using the instruction hierarchy and skill router. It returns the initial
// message list: [optional memory lessons, layered system prompt, user task].
//
// This replaces the old buildAgenticMessages which modified an existing message
// list in-place. The new implementation builds messages from scratch via
// SystemPromptBuilder, separately prepends memory lessons, and appends a user
// message. The legacy seedMessages path is still used by the non-agentic
// command() path in worker_legacy.go.
func (w *Worker) assembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	systemPrompt, userContent := w.buildPromptMessages(task, project, profile)
	messages := []gateway.PromptMessage{
		gateway.PromptMessage{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}
	intent := taskIntent(task)
	return w.prependMemoryLessons(ctx, intent, task.ProjectID, messages)
}

// assembleAgenticSystemPromptWithUserContent builds the layered system prompt and sets the
// anchor user turn to userContent (used after topic drift resets).
func (w *Worker) assembleAgenticSystemPromptWithUserContent(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	userContent string,
) []gateway.PromptMessage {
	// Topic drift resets use the new user input as anchor; skip CODE_PROMPT_BUILDER
	// so code-gen system instructions are not kept after a non-code topic change.
	systemPrompt := w.buildSystemPromptContent(task, project, profile)
	messages := []gateway.PromptMessage{
		gateway.PromptMessage{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}
	intent := taskIntent(task)
	return w.prependMemoryLessons(ctx, intent, task.ProjectID, messages)
}
