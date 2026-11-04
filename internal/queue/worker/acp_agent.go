package worker

import (
	"context"
	"errors"
	"strings"

	"agentd/internal/agent/acp"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/stage"

	acpsdk "github.com/coder/acp-go-sdk"
)

// outcomePrompt ends every prompt. Naming the tool is what made cline call it
// (SP-022); opencode calls it either way.
const outcomePrompt = "When you are done, call the report_outcome tool from the agentd MCP server. " +
	"If a detail the task needs is missing and the workspace cannot answer it, call its ask_human tool instead of guessing."

// repairPrompt is the one follow-up a session that ended with no outcome gets.
const repairPrompt = "You ended your turn without reporting an outcome. Call report_outcome from the agentd MCP server now " +
	"with the result of the task, or ask_human if a detail is missing. Change nothing else."

// ACPAgent runs a stage on an external agent over ACP (T-042): one session on
// Driver, with its own MCP server for the task, through which the agent
// reports. The report is checked, not trusted: a Completed on a task that must
// change the workspace needs evidence of work, as the builtin's B-082 gate does.
type ACPAgent struct {
	Driver *acp.Driver
	Store  models.KanbanStore
	// ProjectID is the task's project, the only one the board reads see.
	ProjectID string
	// Sessions serves each session's MCP server, and MCPURL is where the
	// agent reaches it: "http://" + sandbox.JailMCPAddr from the jail.
	Sessions *agentmcp.SessionMux
	MCPURL   string
	// Permit answers the agent's permission requests (ACPPermitter).
	Permit acp.Permitter
}

var _ stage.Agent = ACPAgent{}

// Call sends the context pack as the prompt and returns what the agent
// reported. A session that ends with no outcome gets one repair prompt, then
// fails with ErrNoOutcome. Only AccessWrite lets the agent write.
func (a ACPAgent) Call(ctx context.Context, req stage.StageRequest) (stage.StageResult, error) {
	if err := req.Validate(); err != nil {
		return stage.StageResult{}, err
	}
	pack := renderContext(req.Context)
	session := agentmcp.NewSession(a.Store, agentmcp.SessionConfig{ProjectID: a.ProjectID, Contract: req.Contract, Context: pack})
	path, remove := a.Sessions.Add(session.HTTPHandler())
	defer remove()
	repaired := false
	run, err := a.Driver.Call(ctx, acp.Request{
		Prompt:     pack + "\n\n" + outcomePrompt,
		MCPServers: []acpsdk.McpServer{{Http: &acpsdk.McpServerHttpInline{Name: "agentd", Type: "http", Url: a.MCPURL + path, Headers: []acpsdk.HttpHeader{}}}},
		Permit:     a.Permit,
		ReadOnly:   req.Access != stage.AccessWrite,
		Followup: func() string {
			if _, err := session.Result(); repaired || !errors.Is(err, agentmcp.ErrNoOutcome) {
				return ""
			}
			repaired = true
			return repairPrompt
		},
	})
	tokens := 0
	if run.Usage != nil {
		tokens = run.Usage.TotalTokens
	}
	if err != nil {
		return stage.StageResult{TokenUsage: tokens}, err
	}
	res, err := session.Result()
	res.TokenUsage = tokens
	if err != nil {
		return res, err
	}
	flagged := req.Attrs.RequiresWorkspaceChange != nil && *req.Attrs.RequiresWorkspaceChange
	// As the builtin's gate, only a run that could write is held to it.
	if res.Outcome == stage.OutcomeCompleted && flagged && run.Evidence == 0 {
		return stage.StageResult{Output: res.Output, TokenUsage: tokens}, stage.ErrUnverifiedCompletion
	}
	return res, nil
}

// renderContext is the context pack as plain text: cline takes no other prompt.
func renderContext(messages []spec.PromptMessage) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
