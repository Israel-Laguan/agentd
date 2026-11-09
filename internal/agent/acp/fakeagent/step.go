package fakeagent

import (
	"encoding/json"
	"fmt"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// Step is one scripted action of a prompt turn. Set one field per step.
type Step struct {
	// Message streams one agent_message_chunk.
	Message string `json:"message,omitempty"`
	// Permission asks the client for permission and then streams
	// "permission: <optionId>" or "permission: cancelled".
	Permission *Permission `json:"permission,omitempty"`
	// WaitCancel blocks until the client cancels the prompt.
	WaitCancel bool `json:"wait_cancel,omitempty"`
	// Cancels streams "cancels: N", the prompts this process saw cancelled.
	Cancels bool `json:"cancels,omitempty"`
	// Stop ends the turn with this stop reason, the way opencode ends one
	// cancelled after a rejected permission.
	Stop acp.StopReason `json:"stop,omitempty"`
	// Error fails the prompt with this message.
	Error string `json:"error,omitempty"`
	// Exit, when not zero, ends the process with this code, mid-prompt.
	Exit int `json:"exit,omitempty"`
	// Tool calls a tool on the session's "agentd" http MCP server and
	// streams "tool: <text>" or "tool error: <text>".
	Tool *Tool `json:"tool,omitempty"`
	// Did reports a tool call of this kind that ran and completed.
	Did acp.ToolKind `json:"did,omitempty"`
	// Usage reports this turn's token counts, as an agent's PromptResponse does.
	Usage *acp.Usage `json:"usage,omitempty"`
	// SleepMS waits this many milliseconds, or until the prompt is cancelled,
	// so a task stays RUNNING long enough for a journey to interrupt it.
	SleepMS int `json:"sleep_ms,omitempty"`
	// Echo completes the task through report_outcome with a summary of the
	// prompt lines that contain this text, so a journey can see what agentd put
	// in the prompt. Script lines are left out, or the script would match itself.
	Echo string `json:"echo,omitempty"`
	// Next is what the session's next prompt that is not a script runs, as
	// an agent answers agentd's own follow-up.
	Next []Step `json:"next,omitempty"`
}

// Tool is the MCP tool call a Tool step makes.
type Tool struct {
	Name string `json:"name"`
	Args any    `json:"args,omitempty"`
}

// Permission is the tool call a Permission step asks about.
type Permission struct {
	Kind     acp.ToolKind `json:"kind"`
	Title    string       `json:"title,omitempty"`
	RawInput any          `json:"raw_input,omitempty"`
}

// Script is the prompt that runs steps.
func Script(steps ...Step) string {
	b, err := json.Marshal(steps)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// echoStep reports a completed outcome whose summary carries the prompt's size
// and its lines containing match, so "absent" is distinguishable from "not read".
func echoStep(match, prompt string) Step {
	var lines []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, match) && !strings.HasPrefix(strings.TrimSpace(line), "[") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	summary := fmt.Sprintf("echo bytes=%d matched=%d: %s", len(prompt), len(lines), strings.Join(lines, " | "))
	return Step{Tool: &Tool{Name: "report_outcome", Args: map[string]any{
		"outcome": "completed", "summary": summary, "result": map[string]any{"summary": summary},
	}}}
}
