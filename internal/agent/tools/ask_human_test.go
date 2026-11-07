package tools

import (
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// A task-type mapping that drops every other tool (summarize: []) still offers
// ask_human, so the agent can ask a human.
func TestToolManifestKeepsAskHumanUnderEmptyMapping(t *testing.T) {
	t.Parallel()
	tools := []gateway.ToolDefinition{
		{Name: ToolNameBash}, {Name: ToolNameRead}, {Name: ToolNameAskHuman},
	}
	m := NewToolManifest(config.ToolManifestConfig{
		Enabled:       true,
		MinConfidence: 0.35,
		Mappings:      map[string][]string{TaskTypeSummarize: {}},
	})
	task := models.Task{Title: "Summarize notes", Description: "recap the changelog"}
	got, _ := m.Filter(tools, nil, task, models.AgentProfile{})
	if len(got) != 1 || got[0].Name != ToolNameAskHuman {
		t.Fatalf("filtered tools = %v, want only ask_human", got)
	}
}
