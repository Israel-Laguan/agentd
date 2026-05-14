package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (w *Worker) seedMessages(ctx context.Context, task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	messages := workerMessages(task, profile)
	if w.retriever == nil {
		return messages
	}
	intent := task.Title + " " + task.Description
	recalled := w.retriever.Recall(ctx, intent, task.ProjectID, "")
	if lessons := memoryFormatLessons(recalled); lessons != "" {
		return append([]gateway.PromptMessage{{Role: "system", Content: lessons}}, messages...)
	}
	return messages
}

func agenticToolUseSystemText() string {
	return `You are an autonomous agent that can execute shell commands, read files, and write files to complete tasks.
When you need to execute a command, use the bash tool.
When you need to read a file, use the read tool.
When you need to create or modify a file, use the write tool.
Return your response as plain text when the task is complete, or use tools to continue working.`
}

func (w *Worker) buildAgenticMessages(messages []gateway.PromptMessage, profile models.AgentProfile) []gateway.PromptMessage {
	toolUse := agenticToolUseSystemText()
	primary := toolUse
	if profile.SystemPrompt.Valid {
		primary = strings.TrimSpace(profile.SystemPrompt.String) + "\n\n" + toolUse
	}

	out := make([]gateway.PromptMessage, 0, len(messages)+1)
	replacedCore := false

	for _, m := range messages {
		if m.Role != "system" {
			out = append(out, m)
			continue
		}
		if isMemoryLessonsSystem(m.Content) {
			out = append(out, m)
			continue
		}
		if isLegacyJSONCommandSystemPrompt(m.Content) {
			if !replacedCore {
				out = append(out, gateway.PromptMessage{Role: "system", Content: primary})
				replacedCore = true
			}
			continue
		}
		if !replacedCore {
			out = append(out, gateway.PromptMessage{Role: "system", Content: primary})
			replacedCore = true
			continue
		}
	}

	if !replacedCore {
		insertIdx := len(out)
		for i, message := range out {
			if message.Role == "user" {
				insertIdx = i
				break
			}
		}
		out = append(out, gateway.PromptMessage{})
		copy(out[insertIdx+1:], out[insertIdx:])
		out[insertIdx] = gateway.PromptMessage{Role: "system", Content: primary}
	}

	return out
}

// assembleAgenticSystemPrompt builds the full layered system prompt for agentic
// mode using the instruction hierarchy and skill router. It returns the initial
// message list: [optional memory lessons, layered system prompt, user task].
func (w *Worker) buildSystemPromptContent(task models.Task, project models.Project, profile models.AgentProfile) string {
	builder := NewSystemPromptBuilder().
		WithGlobal(agenticToolUseSystemText())
	if w.instructionLoader != nil {
		if prefs, err := w.instructionLoader.LoadUserPreferences(); err != nil {
			slog.Warn("failed to load user preferences", "error", err)
		} else if prefs != nil {
			builder.WithUserPreferences(prefs)
		}
	}
	if w.instructionLoader != nil && project.WorkspacePath != "" {
		instructions, err := w.instructionLoader.LoadProjectInstructions(project.WorkspacePath, profile.InstructionsPath)
		if err != nil {
			slog.Warn("failed to load project instructions", "workspace", project.WorkspacePath, "error", err)
		} else if instructions != nil {
			builder.WithProject(instructions)
		}
	}
	if w.skillLoader != nil && w.skillRouter != nil {
		skills, err := w.skillLoader.LoadAll(project.WorkspacePath)
		if err != nil {
			slog.Warn("failed to load skills", "workspace", project.WorkspacePath, "error", err)
		} else if len(skills) > 0 {
			matched := w.skillRouter.Match(task.Description, skills)
			for _, sk := range matched {
				builder.AddSkillBlock(FormatSkillBlock(sk))
			}
			if len(matched) > 0 {
				slog.Debug("injected matched skills into system prompt", "task_id", task.ID, "count", len(matched))
			}
		}
	}
	if profile.SystemPrompt.Valid {
		builder.WithTask(profile.SystemPrompt.String)
	}
	return builder.Build()
}

func (w *Worker) assembleAgenticSystemPrompt(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	systemPrompt := w.buildSystemPromptContent(task, project, profile)
	userMsg := gateway.PromptMessage{
		Role:    "user",
		Content: fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description),
	}
	var messages []gateway.PromptMessage
	if w.retriever != nil {
		intent := task.Title + " " + task.Description
		recalled := w.retriever.Recall(ctx, intent, task.ProjectID, "")
		if lessons := memoryFormatLessons(recalled); lessons != "" {
			messages = append(messages, gateway.PromptMessage{Role: "system", Content: lessons})
		}
	}
	messages = append(messages,
		gateway.PromptMessage{Role: "system", Content: systemPrompt},
		userMsg,
	)
	return messages
}
