package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestBatchUserPromptHasThreeSlots(t *testing.T) {
	t.Parallel()
	if n := countBatchSlots(batchUserPrompt(summarizeTasks(3))); n != 3 {
		t.Fatalf("countBatchSlots = %d, want 3; prompt:\n%s", n, batchUserPrompt(summarizeTasks(3)))
	}
}

func TestRunBatchTextGateway_ValidResponse(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{
			ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true,
		},
	}
	gw := &batchTrackingGateway{}
	w := NewWorker(store, gw, nil, nil, nil, WorkerOptions{
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	tasks := summarizeTasks(3)
	req := w.buildBatchRequest(context.Background(), tasks, store.project, store.profile)
	for i, m := range req.Messages {
		if m.Role == "user" {
			if got := countBatchSlots(m.Content); got != 3 {
				t.Fatalf("message[%d] slot count = %d, want 3; content:\n%s", i, got, m.Content)
			}
		}
	}
	resp, err := w.runBatchTextGateway(context.Background(), tasks, store.project, store.profile)
	if err != nil {
		t.Fatalf("runBatchTextGateway() error = %v", err)
	}
	if err := validateBatchTextResponse(len(tasks), resp); err != nil {
		t.Fatalf("validateBatchTextResponse() error = %v", err)
	}
}

func TestBuildBatchRequest_OmitsAskHumanGuidance(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
	}
	w := NewWorker(store, &batchTrackingGateway{}, nil, nil, nil, WorkerOptions{})
	req := w.buildBatchRequest(context.Background(), summarizeTasks(2), store.project, store.profile)
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "ask_human") {
			t.Fatalf("batch %s message mentions ask_human:\n%s", m.Role, m.Content)
		}
	}
}

// A batch carries the routing attributes its tasks share, so a stage rule on
// exec.batch routes it as it would route each task. A mixed batch leaves the
// attribute unset and takes the stage default: neither rule fits all its tasks.
func TestBuildBatchRequest_CarriesRoutingAttrs(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: route.DefaultAgentID, Provider: "openai", Model: "gpt-4"},
	}
	w := NewWorker(store, &batchTrackingGateway{}, nil, nil, nil, WorkerOptions{
		ToolManifest: config.ToolManifestConfig{Enabled: true, MinConfidence: 0.35},
	})
	yes, no := true, false
	table := route.Table{
		Agents: map[string]route.Agent{"quick": {Provider: "gemini", Model: "flash", Level: spec.LevelL1}},
		Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecBatch: {Default: "quick", Rules: []route.Rule{
			{Match: route.Match{RequiresWorkspaceChange: &yes}, Use: "quick"},
			{Match: route.Match{RequiresWorkspaceChange: &no}, Use: "quick"},
		}}},
	}
	cases := []struct {
		name    string
		flagged []bool
		rule    string
	}{
		{"no task flagged", []bool{false, false, false}, "requires_workspace_change=false"},
		{"every task flagged", []bool{true, true, true}, "requires_workspace_change=true"},
		{"mixed batch", []bool{false, true, false}, route.RuleDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := summarizeTasks(len(tc.flagged))
			for i := range tasks {
				tasks[i].RequiresWorkspaceChange = tc.flagged[i]
			}
			req := w.buildBatchRequest(context.Background(), tasks, store.project, store.profile)
			if d := table.Resolve(req.Purpose, req.AgentID, req.Attrs); d.Rule != tc.rule {
				t.Fatalf("rule = %q, want %q for flagged = %v", d.Rule, tc.rule, tc.flagged)
			}
		})
	}
}
