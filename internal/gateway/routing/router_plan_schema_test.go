package routing

import (
	"context"
	"strings"
	"sync"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type recordingSeqProvider struct {
	mu        sync.Mutex
	responses []string
	requests  []spec.AIRequest
}

func (p *recordingSeqProvider) Name() spec.Provider { return "openai" }
func (p *recordingSeqProvider) MaxInputChars() int  { return 100000 }
func (p *recordingSeqProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}
func (p *recordingSeqProvider) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	i := len(p.requests) - 1
	if i >= len(p.responses) {
		i = len(p.responses) - 1
	}
	return spec.AIResponse{Content: p.responses[i], ProviderUsed: "openai"}, nil
}

func TestPhasePlanningPrompt_StatesDraftPlanSchema(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, 3} {
		got := phasePlanningPrompt(max)
		for _, want := range []string{"project_name", "tasks", "title", "description", "assignee"} {
			if !strings.Contains(got, want) {
				t.Fatalf("phasePlanningPrompt(%d) missing %q:\n%s", max, want, got)
			}
		}
	}
}

func TestGeneratePlan_WrongShapeThenCorrectionCarriesSchema(t *testing.T) {
	t.Parallel()
	prov := &recordingSeqProvider{responses: []string{
		`{"project":"machine-inspection","report":{"os":"x"}}`,
		`{"project_name":"machine-inspection","tasks":[{"title":"Inspect host","description":"run uname","assignee":"SYSTEM"}]}`,
	}}
	plan, err := NewRouter(prov).GeneratePlan(context.Background(), "inspect this machine")
	if err != nil {
		t.Fatalf("GeneratePlan() error = %v", err)
	}
	if plan.ProjectName != "machine-inspection" || len(plan.Tasks) != 1 {
		t.Fatalf("plan = %+v, want second-attempt plan", plan)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(prov.requests))
	}
	msgs := prov.requests[1].Messages
	last := msgs[len(msgs)-1].Content
	for _, want := range []string{"project name is required", "project_name", "tasks", "title", "assignee"} {
		if !strings.Contains(last, want) {
			t.Fatalf("correction message missing %q:\n%s", want, last)
		}
	}
}

func TestGenerateStructuredJSON_DraftPlanTargetCarriesSchema(t *testing.T) {
	t.Parallel()
	prov := &recordingSeqProvider{responses: []string{
		`{"project":"x","plan":[]}`,
		`{"project_name":"host-report","tasks":[{"title":"t","description":"d","assignee":"SYSTEM"}]}`,
	}}
	var draft models.DraftPlan
	if err := NewRouter(prov).GenerateStructuredJSON(context.Background(), "inspect", &draft); err != nil {
		t.Fatalf("GenerateStructuredJSON() error = %v", err)
	}
	if draft.ProjectName != "host-report" || len(prov.requests) != 2 {
		t.Fatalf("draft = %+v, calls = %d", draft, len(prov.requests))
	}
	if sys := prov.requests[0].Messages[0].Content; !strings.Contains(sys, "project_name") {
		t.Fatalf("system prompt lacks schema: %s", sys)
	}
}

func TestPhasePlanningPrompt_GuidesHostFactPlans(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, 3} {
		got := phasePlanningPrompt(max)
		for _, want := range []string{"hostnamectl", "lscpu", "report.md", "/etc, /proc or /sys"} {
			if !strings.Contains(got, want) {
				t.Fatalf("phasePlanningPrompt(%d) missing %q:\n%s", max, want, got)
			}
		}
	}
}
