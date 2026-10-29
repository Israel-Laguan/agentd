package worker

import (
	"context"
	"fmt"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/memory"
	"agentd/internal/models"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	wskills "agentd/internal/agent/skills"
)

// appendMemoryLessons appends optional memory context after the stable system
// prompt + task seed. Placing it after the stable context keeps the cache
// prefix stable across tasks while still surfacing durable memories and the
// requesting user's saved preferences.
//
// userID scopes the recall to one identity. It comes from the project
// (models.Project.UserID, stamped at materialization from the X-Agentd-User
// header) because nothing on the task itself carries it: phase continuations,
// breakdowns, and handoff children are all created long after the chat turn
// that identified the user, and all inherit the project's identity.
func (w *Worker) appendMemoryLessons(ctx context.Context, intent string, projectID string, userID string, messages []gateway.PromptMessage) []gateway.PromptMessage {
	if w.retriever == nil {
		return messages
	}
	recalled := w.retriever.Recall(ctx, intent, projectID, userID)
	if lessons := memory.FormatLessons(recalled); lessons != "" {
		messages = append(messages, gateway.PromptMessage{Role: "system", Content: lessons, Section: gateway.SectionMemories})
	}
	// Preferences are formatted separately from lessons: memoryFormatLessons
	// deliberately skips USER_PREFERENCE rows, so without this they would be
	// recalled (when userID is set) but silently dropped before the prompt.
	// memory.FormatPreferences is shared with the chat planner so both render
	// preferences identically.
	if prefs := memory.FormatPreferences(recalled); prefs != "" {
		messages = append(messages, gateway.PromptMessage{Role: "system", Content: prefs, Section: gateway.SectionMemories})
	}
	return messages
}

// askHumanGuidance tells the agentic model when to call ask_human. A batch
// request offers no tools and cannot suspend, so buildBatchRequest drops it.
const askHumanGuidance = "When a detail the task needs is missing and the workspace cannot answer it, call ask_human with one question instead of guessing; the task waits for a human's answer."

func agenticToolUseSystemText(readOnly bool, goal ...*agentcontext.AgentGoal) string {
	text := `You are an autonomous agent that can execute shell commands, read files, and write files to complete tasks.
When you need to execute a command, use the bash tool.
When you need to read a file, use the read tool.
When you need to create or modify a file, use the write tool.
Return your response as plain text when the task is complete, or use tools to continue working.
` + askHumanGuidance + `
Never retry a command that the sandbox blocked for sudo or elevated privileges. Try at most one safe non-privileged alternative, then report that no alternative exists if it cannot complete the task.
The project workspace is the current directory: use relative paths (for example lscpu > cpu.txt), never /workspace or any other absolute path. Commands run inside the workspace and may not touch paths outside it (except /dev/null), so never read /etc, /proc or /sys files.
For host facts (OS, kernel, CPU, memory, disks) run commands that need no paths: uname -a, hostnamectl, lsb_release -a, lscpu, free -h, lsblk, df -h. A report may state only values printed by commands you ran in this run; never type the facts yourself.` + hostFactsSuffix(readOnly) + `

` + agenthooks.ExternalContentInstruction
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

func hostFactsSuffix(readOnly bool) string {
	if readOnly {
		return ""
	}
	return " Save each command's output to a file (for example uname -a > os.txt) so a later step can build report.md from it."
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
	intent := task.Title + " " + task.Description
	matched := w.skillRouter.Match(intent, skills)
	for _, sk := range matched {
		builder.AddSkillBlock(wskills.FormatSkillBlock(sk))
	}
	if len(matched) > 0 {
		slog.Debug("injected matched skills into system prompt", "task_id", task.ID, "count", len(matched))
	}
}

func (w *Worker) buildSystemPromptContent(task models.Task, project models.Project, profile models.AgentProfile) string {
	var goal *agentcontext.AgentGoal
	if g := agentcontext.GoalFromTask(task); g != nil {
		goal = g
	}
	builder := NewSystemPromptBuilder().
		WithGlobal(agenticToolUseSystemText(profile.WorkspaceReadonly, goal))
	w.enrichBuilderUserPreferences(builder)
	w.enrichBuilderProjectInstructions(builder, project, profile)
	w.enrichBuilderMatchedSkills(builder, task, project)
	if profile.SystemPrompt.Valid {
		builder.WithTask(profile.SystemPrompt.String)
	}
	return builder.Build()
}

// assembleAgenticSystemPrompt builds the full layered system prompt for agentic
// mode using the instruction hierarchy and skill router. It returns the initial
// message list: [layered system prompt, user task, optional memory lessons].
//
// Memory lessons are appended after the stable task seed so the provider
// prompt-cache prefix remains stable across tasks (only the dynamic lessons
// message changes between tasks with the same profile/task).
//
// This replaces the old buildAgenticMessages which modified an existing message
// list in-place. The new implementation builds messages from scratch via
// SystemPromptBuilder, appends a user message, then appends memory lessons.
func (w *Worker) AssembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	systemPrompt := w.buildSystemPromptContent(task, project, profile)
	userContent := fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: systemPrompt, Section: gateway.SectionSystem},
		{Role: "user", Content: userContent, Section: gateway.SectionTask},
	}
	intent := task.Title + " " + task.Description
	return w.appendMemoryLessons(ctx, intent, task.ProjectID, project.UserID, messages)
}

// assembleAgenticSystemPromptWithUserContent builds the layered system prompt and sets the
// anchor user turn to userContent (used after topic drift resets).
func (w *Worker) AssembleAgenticSystemPromptWithUserContent(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	userContent string,
) []gateway.PromptMessage {
	// Topic drift resets use the new user input as anchor.
	systemPrompt := w.buildSystemPromptContent(task, project, profile)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: systemPrompt, Section: gateway.SectionSystem},
		{Role: "user", Content: userContent, Section: gateway.SectionTask},
	}
	intent := task.Title + " " + task.Description
	return w.appendMemoryLessons(ctx, intent, task.ProjectID, project.UserID, messages)
}
