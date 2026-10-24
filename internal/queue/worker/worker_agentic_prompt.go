package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	"agentd/internal/queue/worker/agentic"
	"agentd/internal/sandbox"
)

// sandboxOutput is the part of a failed bash tool result that prompt detection
// reads. It is the envelope SandboxTimeoutJSON and SandboxFailureJSON write.
type sandboxOutput struct {
	TimedOut bool   `json:"TimedOut"`
	ExitCode int    `json:"ExitCode"`
	Stdout   string `json:"Stdout"`
	Stderr   string `json:"Stderr"`
}

// AgenticPromptHang reports whether a bash call stopped at an interactive prompt:
// the sandbox timed it out, and the output it printed shows a prompt. It returns
// the feedback the model gets for a first hang, which names a non-interactive
// rerun when one exists.
func (w *Worker) AgenticPromptHang(call gateway.ToolCall, tr agenttools.ToolResult) (agentic.PromptHang, bool) {
	if call.Function.Name != agenttools.ToolNameBash || tr.Status == agenttools.ToolStatusSuccess {
		return agentic.PromptHang{}, false
	}
	var out sandboxOutput
	if err := json.Unmarshal([]byte(tr.Content), &out); err != nil || !out.TimedOut {
		return agentic.PromptHang{}, false
	}
	detection := safety.DetectPrompt(out.Stdout, out.Stderr)
	if !detection.Detected {
		return agentic.PromptHang{}, false
	}
	command := bashCommandOf(call)
	result := sandbox.Result{ExitCode: out.ExitCode, Stdout: out.Stdout, Stderr: out.Stderr, TimedOut: true}
	feedback := fmt.Sprintf("The command stopped at an interactive prompt (%s) and was stopped. Rerun it non-interactively: use -y, --yes or --assume-yes, and never answer a prompt.", detection.Pattern)
	if recoverable, recovered := recovery.CanRecover(command); recoverable {
		feedback += " For example: " + recovered
	}
	return agentic.PromptHang{
		Command:  command,
		Payload:  truncate(detectionPayload(detection.Pattern, command, result), 1000),
		Feedback: feedback,
	}, true
}

// HandlePromptHandoff blocks the task for a human after the same command hung at
// a prompt again.
func (w *Worker) HandlePromptHandoff(ctx context.Context, task models.Task, payload string) {
	if fresh, err := w.store.GetTask(ctx, task.ID); err == nil && fresh != nil {
		task = *fresh
	}
	w.createPromptHandoff(ctx, task, payload)
}

// bashCommandOf returns the command a bash call was sent, or "" when the
// arguments carry none.
func bashCommandOf(call gateway.ToolCall) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.Command)
}
