package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	agenthooks "agentd/internal/agent/hooks"
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

// AgenticPrivilegeBlock reports the command of a failed bash call whose output
// shows a permission failure, sudo refusals included.
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
	return args.Command, permissionFailure(result).Detected
}

// permissionFailure classifies a failed tool result's output as a permission
// failure.
func permissionFailure(result agenttools.ToolResult) safety.PermissionDetection {
	if isDenylistSudoVeto(result) {
		return safety.PermissionDetection{Detected: true, Blocked: true, Pattern: "denylist sudo"}
	}
	errorOutput := ""
	if result.Error != nil {
		errorOutput = result.Error.Message
	}
	return safety.DetectPermission(result.Content, errorOutput)
}

// isDenylistSudoVeto reports whether the denylist refused a sudo command. The
// denylist runs before the sandbox, so its veto is the sudo refusal the sandbox
// would otherwise make (J19).
func isDenylistSudoVeto(result agenttools.ToolResult) bool {
	if result.Status != agenttools.ToolStatusVetoed {
		return false
	}
	reason := agenthooks.DenylistSudoReason
	return result.Content == reason || (result.Error != nil && result.Error.Message == reason)
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
		Purpose: gateway.PurposeExecPermissionAlt,
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

func (w *Worker) RecoverAgenticPrivilege(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	command string,
	blockedResult agenttools.ToolResult,
	allowAlternative bool,
	dispatch func(gateway.ToolCall) (agenttools.ToolResult, bool),
) (agenttools.ToolResult, bool) {
	// Only a sudo refusal by the sandbox itself may get a non-privileged
	// alternative. Any other permission failure goes straight to a human.
	sudoRefusal := permissionFailure(blockedResult).Blocked
	if allowAlternative && sudoRefusal {
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
		if alternativeResult.Status == agenttools.ToolStatusSuccess || isSandboxUnavailableResult(alternativeResult) {
			// A refusal by an unavailable required jail is returned to the loop,
			// which fails the task terminally; a human handoff cannot fix it.
			return alternativeResult, false
		}
		outcome := "Non-privileged alternative failed: " + w.safePermissionText(alternativeResult.Content, 300)
		w.handlePermissionFailure(ctx, task, command, outcome)
		return agenttools.ToolResult{}, true
	}

	outcome := "Privileged command blocked before execution."
	if !sudoRefusal {
		outcome = "The sandbox reported a permission failure; no non-privileged recovery was attempted."
	}
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
