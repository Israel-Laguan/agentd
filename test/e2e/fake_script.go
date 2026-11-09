//go:build e2e

package e2e

import (
	"agentd/internal/agent/acp/fakeagent"

	acpsdk "github.com/coder/acp-go-sdk"
)

// fakeCompleted is the fake agent's script for a task that finishes: the given
// steps, one completed edit (so a task that must change the workspace is not
// rejected as unverified), then report_outcome completed with summary. It goes
// in the task title or description, which agentd never cuts from the prompt.
func fakeCompleted(summary string, before ...fakeagent.Step) string {
	steps := append(before,
		fakeagent.Step{Did: acpsdk.ToolKindEdit},
		fakeagent.Step{Tool: &fakeagent.Tool{Name: "report_outcome", Args: map[string]any{
			"outcome": "completed", "summary": summary, "result": map[string]any{"summary": summary},
		}}},
	)
	return fakeagent.Script(steps...)
}

// fakeTask is a task whose description runs fakeCompleted.
func fakeTask(title, summary string, before ...fakeagent.Step) DraftTask {
	return DraftTask{Title: title, Description: fakeCompleted(summary, before...)}
}
