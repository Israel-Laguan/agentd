package frontdesk

import (
	"context"
	"strings"
	"sync"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/routing"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// recordingProvider replays canned responses and records every request, so
// tests can assert what the model was actually shown.
type recordingProvider struct {
	mu        sync.Mutex
	responses []string
	requests  []spec.AIRequest
}

func (p *recordingProvider) Name() spec.Provider { return "openai" }
func (p *recordingProvider) MaxInputChars() int  { return 100000 }
func (p *recordingProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (p *recordingProvider) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	i := len(p.requests) - 1
	if i >= len(p.responses) {
		i = len(p.responses) - 1
	}
	return spec.AIResponse{Content: p.responses[i], ProviderUsed: "openai"}, nil
}

const (
	wrongShapePlan = `{"project":"System Information Query","plan":[{"task":"x","details":"y"}]}`
	validPlan      = `{"project_name":"host-report","tasks":[{"title":"Inspect host","description":"run uname","assignee":"SYSTEM"}]}`
)

// assertSchemaReachedModel checks that the first request carried the DraftPlan
// schema in its system prompt and the retry carried it in the correction.
func assertSchemaReachedModel(t *testing.T, prov *recordingProvider) {
	t.Helper()
	if len(prov.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2 (wrong shape, then corrected)", len(prov.requests))
	}
	first := prov.requests[0].Messages
	if first[0].Role != "system" || !strings.Contains(first[0].Content, "project_name") {
		t.Fatalf("system prompt lacks DraftPlan schema:\n%s", first[0].Content)
	}
	retry := prov.requests[1].Messages
	last := retry[len(retry)-1].Content
	if !strings.Contains(last, "project name is required") || !strings.Contains(last, "project_name") {
		t.Fatalf("correction message lacks schema hint:\n%s", last)
	}
}

func TestPlanApprovedScope_ContractAdapterSendsDraftPlanSchema(t *testing.T) {
	prov := &recordingProvider{responses: []string{wrongShapePlan, validPlan}}
	p := &Planner{Gateway: routing.NewRouter(prov)}
	out, err := p.planApprovedScope(context.Background(), "what os and hardware have this machine", "", nil)
	if err != nil {
		t.Fatalf("planApprovedScope() error = %v", err)
	}
	if !strings.Contains(string(out), "host-report") {
		t.Fatalf("plan = %s, want the corrected plan", out)
	}
	assertSchemaReachedModel(t, prov)
}

func TestIntakeProcess_ContractAdapterSendsDraftPlanSchema(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d", Tasks: []models.DraftTask{{Title: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	if _, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateInConsideration); err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	prov := &recordingProvider{responses: []string{wrongShapePlan, validPlan}}
	proc := NewIntakeProcessor(store, routing.NewRouter(prov), nil, nil, 0)
	if err := proc.Process(ctx, models.CommentRef{TaskID: task.ID, CommentEventID: "c1", Body: "also check disks"}); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	assertSchemaReachedModel(t, prov)
}
