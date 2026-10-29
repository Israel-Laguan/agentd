package agentic

import (
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// selectHost offers bash only to a task whose description asks for a command,
// like a manifest that classifies by description, and routes from what it is given.
type selectHost struct {
	noopHost
}

func (h *selectHost) FilterAgenticTools(tools []gateway.ToolDefinition, index map[string]string, task models.Task, _ models.AgentProfile) ([]gateway.ToolDefinition, map[string]string) {
	if strings.Contains(task.Description, "run a command") {
		return tools, index
	}
	return nil, nil
}

// A clarified task is selected from the full tool set, not from the set the
// unclarified task was narrowed to, and is routed from the unrouted profile.
func TestSelectAgenticToolsFollowsTheClarifiedTask(t *testing.T) {
	t.Parallel()
	host := &selectHost{}
	e := NewEngine(Config{}, host)
	all := []gateway.ToolDefinition{{Name: "bash"}, {Name: "read"}}
	index := map[string]string{"bash": "builtin"}
	base := models.AgentProfile{Provider: "openai", Model: "gpt-4"}

	tools, _, _ := e.selectAgenticTools(models.Task{Description: "summarize"}, base, nil, all, index)
	if len(tools) != 0 {
		t.Fatalf("tools for the unclarified task = %d, want 0", len(tools))
	}

	clarified := models.Task{Description: "summarize\nrun a command"}
	tools, gotIndex, routed := e.selectAgenticTools(clarified, base, nil, all, index)
	if len(tools) != 2 || gotIndex["bash"] != "builtin" {
		t.Fatalf("tools for the clarified task = %d, index = %v, want the full set", len(tools), gotIndex)
	}
	if routed.Model != base.Model {
		t.Fatalf("selected model = %q, want profile model %q", routed.Model, base.Model)
	}
}
