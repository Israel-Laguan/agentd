package tools

import (
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestParseAskHumanArgsTrimsAndRequiresQuestion(t *testing.T) {
	t.Parallel()
	args, err := ParseAskHumanArgs(`{"question":"  Which format?  ","options":["CSV","JSON"]}`)
	if err != nil {
		t.Fatalf("ParseAskHumanArgs: %v", err)
	}
	if args.Question != "Which format?" || len(args.Options) != 2 {
		t.Fatalf("args = %+v, want the trimmed question and both options", args)
	}
	args, err = ParseAskHumanArgs(`{"question":"Which format?","options":[" CSV ",""," ","JSON"]}`)
	if err != nil {
		t.Fatalf("ParseAskHumanArgs: %v", err)
	}
	if len(args.Options) != 2 || args.Options[0] != "CSV" || args.Options[1] != "JSON" {
		t.Fatalf("options = %q, want the trimmed non-blank options [CSV JSON]", args.Options)
	}
	for _, raw := range []string{`{}`, `{"question":"   "}`, `not json`} {
		if _, err := ParseAskHumanArgs(raw); err == nil {
			t.Fatalf("ParseAskHumanArgs(%s) error = nil, want an error", raw)
		}
	}
}

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
