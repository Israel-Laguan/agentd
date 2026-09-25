package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

const permissionHandoffAction = "Run the original command in your own terminal with appropriate privileges, then record its output through explicit human resolution. agentd will not execute the privileged command."

var sudoTokenPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])sudo(?:$|[^a-z0-9_])`)

type permissionAlternative struct {
	Command       string `json:"command,omitempty"`
	NoAlternative bool   `json:"no_alternative,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type permissionHandoffPayload struct {
	OriginalCommand    string `json:"original_command"`
	AlternativeOutcome string `json:"alternative_outcome"`
	RequiredAction     string `json:"required_action"`
}

func containsSudoToken(command string) bool {
	return sudoTokenPattern.MatchString(command)
}

func (w *Worker) AgenticPrivilegeBlock(call gateway.ToolCall, result agenttools.ToolResult) (string, bool) {
	if call.Function.Name != agenttools.ToolNameBash || result.Status == agenttools.ToolStatusSuccess {
		return "", false
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || strings.TrimSpace(args.Command) == "" {
		return "", false
	}
	errorOutput := ""
	if result.Error != nil {
		errorOutput = result.Error.Message
	}
	detection := safety.DetectPermission(result.Content, errorOutput)
	return args.Command, detection.Blocked
}

func (w *Worker) isPrivilegeBlock(result sandbox.Result, err error) bool {
	detection := safety.DetectPermission(result.Stdout, result.Stderr)
	return err != nil && errors.Is(err, models.ErrSandboxViolation) && detection.Blocked
}

func (w *Worker) requestPermissionAlternative(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	command string,
) (permissionAlternative, int, gateway.UsageDetails, error) {
	var alternative permissionAlternative
	if w.gateway == nil {
		return alternative, 0, gateway.UsageDetails{}, errors.New("gateway is unavailable")
	}

	original := w.safePermissionText(command, 500)
	taskText := w.safePermissionText(task.Title+"\n"+task.Description, 1000)
	response, err := w.gateway.Generate(ctx, gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: "Return exactly one JSON object. Provide one safe non-privileged shell command that can complete the same objective, or set no_alternative=true with a brief reason. Never include sudo or any command requiring elevated privileges."},
			{Role: "user", Content: "Task:\n" + taskText + "\n\nBlocked command:\n" + original},
		},
		Temperature: profile.Temperature,
		JSONMode:    true,
		AgentID:     task.AgentID,
		Role:        gateway.RoleWorker,
		TaskID:      task.ID,
		Provider:    profile.Provider,
		Model:       profile.Model,
		MaxTokens:   profile.MaxTokens,
	})
	if err != nil {
		return alternative, 0, gateway.UsageDetails{}, err
	}
	if response.TokenUsage > 0 && w.budgetTracker != nil {
		w.budgetTracker.Add(task.ID, response.TokenUsage)
	}
	if err := json.Unmarshal([]byte(response.Content), &alternative); err != nil {
		return permissionAlternative{}, response.TokenUsage, valueOrEmptyUsage(response.UsageDetails), err
	}
	alternative.Command = strings.TrimSpace(alternative.Command)
	alternative.Reason = strings.TrimSpace(alternative.Reason)
	if alternative.NoAlternative || alternative.Command == "" {
		return permissionAlternative{NoAlternative: true, Reason: alternative.Reason}, response.TokenUsage, valueOrEmptyUsage(response.UsageDetails), nil
	}
	return alternative, response.TokenUsage, valueOrEmptyUsage(response.UsageDetails), nil
}

func valueOrEmptyUsage(details *gateway.UsageDetails) gateway.UsageDetails {
	if details == nil {
		return gateway.UsageDetails{}
	}
	return *details
}

func (w *Worker) recoverLegacyPrivilege(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	command string,
	audit *legacyTaskAudit,
) {
	alternative, tokens, details, err := w.requestPermissionAlternative(ctx, task, profile, command)
	w.RecordTaskTokenUsage(ctx, task, tokens, details)
	if err != nil {
		w.handlePermissionFailure(ctx, task, command, "Alternative request failed: "+w.safePermissionText(err.Error(), 300))
		return
	}
	if alternative.NoAlternative {
		outcome := "No non-privileged alternative was available."
		if alternative.Reason != "" {
			outcome += " Reason: " + w.safePermissionText(alternative.Reason, 300)
		}
		w.handlePermissionFailure(ctx, task, command, outcome)
		return
	}
	if containsSudoToken(alternative.Command) {
		w.handlePermissionFailure(ctx, task, command, "Rejected alternative because it attempted privileged execution: "+w.safePermissionText(alternative.Command, 300))
		return
	}

	result, runErr := w.sandbox.Execute(ctx, w.payload(task, project, alternative.Command))
	audit.exitCode = result.ExitCode
	if runErr == nil && result.Success {
		audit.command = alternative.Command
		slog.Info("permission recovery: non-privileged alternative succeeded", "task_id", task.ID, "project_id", task.ProjectID)
		if profile.RequireReview {
			w.createReviewHandoff(ctx, task, result.Stdout)
			return
		}
		w.commit(ctx, task, result, nil)
		return
	}
	audit.failed = true
	slog.Warn("permission recovery: non-privileged alternative failed", "task_id", task.ID, "project_id", task.ProjectID, "exit_code", result.ExitCode)
	outcome := "Non-privileged alternative failed"
	if result.Stderr != "" {
		outcome += ": " + w.safePermissionText(result.Stderr, 300)
	} else if runErr != nil {
		outcome += ": " + w.safePermissionText(runErr.Error(), 300)
	}
	w.handlePermissionFailure(ctx, task, command, outcome)
}

func (w *Worker) RecoverAgenticPrivilege(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	command string,
	blockedResult agenttools.ToolResult,
	allowAlternative bool,
	dispatch func(gateway.ToolCall) (agenttools.ToolResult, bool),
) (agenttools.ToolResult, bool) {
	if allowAlternative {
		alternative, tokens, details, err := w.requestPermissionAlternative(ctx, task, profile, command)
		w.RecordTaskTokenUsage(ctx, task, tokens, details)
		if err != nil {
			w.handlePermissionFailure(ctx, task, command, "Alternative request failed: "+w.safePermissionText(err.Error(), 300))
			return agenttools.ToolResult{}, true
		}
		if alternative.NoAlternative {
			outcome := "No non-privileged alternative was available."
			if alternative.Reason != "" {
				outcome += " Reason: " + w.safePermissionText(alternative.Reason, 300)
			}
			w.handlePermissionFailure(ctx, task, command, outcome)
			return agenttools.ToolResult{}, true
		}
		if containsSudoToken(alternative.Command) {
			w.handlePermissionFailure(ctx, task, command, "Rejected alternative because it attempted privileged execution: "+w.safePermissionText(alternative.Command, 300))
			return agenttools.ToolResult{}, true
		}
		arguments, marshalErr := json.Marshal(map[string]string{"command": alternative.Command})
		if marshalErr != nil {
			w.handlePermissionFailure(ctx, task, command, "Could not encode the non-privileged alternative.")
			return agenttools.ToolResult{}, true
		}
		alternativeCall := gateway.ToolCall{ID: blockedResult.CallID, Type: "function", Function: gateway.ToolCallFunction{Name: agenttools.ToolNameBash, Arguments: string(arguments)}}
		alternativeResult, suspended := dispatch(alternativeCall)
		if suspended {
			return agenttools.ToolResult{}, true
		}
		if alternativeResult.Status == agenttools.ToolStatusSuccess {
			return alternativeResult, false
		}
		outcome := "Non-privileged alternative failed: " + w.safePermissionText(alternativeResult.Content, 300)
		w.handlePermissionFailure(ctx, task, command, outcome)
		return agenttools.ToolResult{}, true
	}

	outcome := "Privileged command blocked before execution."
	w.handlePermissionFailure(ctx, task, command, outcome)
	return agenttools.ToolResult{}, true
}

func (w *Worker) handlePermissionFailure(ctx context.Context, task models.Task, command, alternativeOutcome string) {
	if fresh, err := w.store.GetTask(ctx, task.ID); err == nil && fresh != nil {
		task = *fresh
	}
	original := w.safePermissionText(command, 500)
	outcome := w.safePermissionText(alternativeOutcome, 500)
	slog.Info("permission failure: creating human handoff", "task_id", task.ID, "project_id", task.ProjectID, "outcome", outcome)

	payload := permissionHandoffPayload{
		OriginalCommand:    original,
		AlternativeOutcome: outcome,
		RequiredAction:     permissionHandoffAction,
	}
	encoded, _ := json.Marshal(payload)
	w.Emit(ctx, task, string(models.EventTypePermissionDetected), string(encoded))
	description := FormatForHuman(HITLMessage{
		Summary: "Privileged command requires a human operator.",
		Action:  permissionHandoffAction,
		Urgency: "blocking",
		Detail: fmt.Sprintf(
			"Original blocked command:\n```text\n%s\n```\n\nAlternative outcome:\n%s",
			safeCommandFence(original), outcome,
		),
	})
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleManualAction + " privileged command",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		slog.Error("permission failure: block task with subtasks failed", "task_id", task.ID, "error", err)
		w.Emit(ctx, task, "ERROR", err.Error())
		return
	}
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
	slog.Info("permission failure: human handoff created", "task_id", task.ID, "project_id", task.ProjectID)
	w.Emit(ctx, task, string(models.EventTypePermissionHandoff), string(encoded))
}

func (w *Worker) safePermissionText(value string, limit int) string {
	scrubber := w.sandboxScrubber
	if scrubber == nil {
		scrubber = sandbox.NewScrubber(nil)
	}
	return truncate(scrubber.Scrub(strings.TrimSpace(value)), limit)
}

func safeCommandFence(command string) string {
	return strings.ReplaceAll(command, "```", "'''")
}
