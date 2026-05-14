package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

func (cm *ContextManager) generateSummary(ctx context.Context, turns []Turn) (TurnSummary, error) {
	var b strings.Builder
	b.WriteString("Summarize the following interaction turns into the required structured format.\n\n")
	for i, t := range turns {
		fmt.Fprintf(&b, "--- TURN %d ---\n", i+1)
		for _, m := range t.Messages {
			content := m.Content
			contentRunes := []rune(content)
			if len(contentRunes) > 1000 {
				content = string(contentRunes[:1000]) + "... [truncated for summary]"
			}
			fmt.Fprintf(&b, "[%s]: %s\n", m.Role, content)
			for _, tc := range m.ToolCalls {
				args := tc.Function.Arguments
				argsRunes := []rune(args)
				if len(argsRunes) > 500 {
					args = string(argsRunes[:500]) + "... [truncated for summary]"
				}
				fmt.Fprintf(&b, "(Tool Call: %s, Args: %s)\n", tc.Function.Name, args)
			}
		}
	}
	req := gateway.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "You are a concise summarizer for an autonomous agent. Extract key information from the provided turns into a structured JSON summary. Focus on decisions, facts, completed work, and remaining work. If files were modified or errors occurred, list them."},
			{Role: "user", Content: b.String()},
		},
		JSONMode:  true,
		AgentID:   cm.agentID,
		TaskID:    cm.taskID,
		Role:      gateway.RoleMemory,
		MaxTokens: 2000,
	}
	return gateway.GenerateJSON[TurnSummary](ctx, cm.gateway, req)
}

func (cm *ContextManager) formatSummary(s TurnSummary) string {
	var b strings.Builder
	b.WriteString("PREVIOUS CONTEXT SUMMARY (Compressed):\n")
	if len(s.DecisionsMade) > 0 {
		b.WriteString("- Decisions Made: " + strings.Join(s.DecisionsMade, "; ") + "\n")
	}
	if len(s.FactsEstablished) > 0 {
		b.WriteString("- Facts Established: " + strings.Join(s.FactsEstablished, "; ") + "\n")
	}
	if len(s.WorkCompleted) > 0 {
		b.WriteString("- Work Completed: " + strings.Join(s.WorkCompleted, "; ") + "\n")
	}
	if len(s.WorkRemaining) > 0 {
		b.WriteString("- Work Remaining: " + strings.Join(s.WorkRemaining, "; ") + "\n")
	}
	if len(s.FilesModified) > 0 {
		b.WriteString("- Files Modified: " + strings.Join(s.FilesModified, "; ") + "\n")
	}
	if len(s.ErrorsEncountered) > 0 {
		b.WriteString("- Errors Encountered: " + strings.Join(s.ErrorsEncountered, "; ") + "\n")
	}
	return b.String()
}
