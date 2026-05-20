package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestValidateOutputSections(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []PlanStep{
		{ID: "a", OutputFormat: "text"},
		{ID: "b", OutputFormat: "json"},
		{ID: "c", OutputFormat: "list"},
	}}
	ok := `<!-- step:a -->
hello
<!-- /step:a -->
<!-- step:b -->
{"k":1}
<!-- /step:b -->
<!-- step:c -->
- one
<!-- /step:c -->
`
	if fail := ValidateOutput(ok, plan); len(fail) != 0 {
		t.Fatalf("valid output: failing %v", fail)
	}
	badJSON := strings.Replace(ok, `{"k":1}`, `not json`, 1)
	if fail := ValidateOutput(badJSON, plan); len(fail) != 1 || fail[0].ID != "b" {
		t.Fatalf("json fail = %v", fail)
	}
	missing := strings.Replace(ok, "<!-- step:c -->", "", 1)
	if fail := ValidateOutput(missing, plan); len(fail) == 0 {
		t.Fatal("expected missing section failure")
	}
}

func TestExtractReplaceSection(t *testing.T) {
	t.Parallel()
	out := `<!-- step:x -->
old
<!-- /step:x -->
tail`
	body, ok := extractSection(out, "x")
	if !ok || body != "old" {
		t.Fatalf("extract = %q ok=%v", body, ok)
	}
	replaced := replaceSection(out, "x", "new")
	body2, ok := extractSection(replaced, "x")
	if !ok || body2 != "new" {
		t.Fatalf("replace = %q ok=%v", body2, ok)
	}
	if !strings.Contains(replaced, "tail") {
		t.Fatal("tail lost after replace")
	}
}

type redoCountGateway struct {
	redoCalls map[string]int
	requests  []gateway.AIRequest
}

func (g *redoCountGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	user := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(user, "Repair ONLY step") {
		for id := range g.redoCalls {
			if strings.Contains(user, "Repair ONLY step \""+id+"\"") {
				g.redoCalls[id]++
			}
		}
		// Always return invalid body so validation keeps failing.
		return gateway.AIResponse{Content: ""}, nil
	}
	return gateway.AIResponse{Content: "ok"}, nil
}

func (g *redoCountGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, nil
}
func (g *redoCountGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}
func (g *redoCountGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (g *redoCountGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func TestRepairLoop_CapsAtThreePasses(t *testing.T) {
	t.Parallel()
	gw := &redoCountGateway{redoCalls: map[string]int{"only": 0}}
	w := &Worker{
		gateway: gw,
		planningCfg: config.AgenticPlanningConfig{
			ComplexityThreshold: 1,
			MaxRedoPasses:       3,
		},
	}
	plan := &Plan{Steps: []PlanStep{{ID: "only", Action: "x", OutputFormat: "text"}}}
	out := "<!-- step:only -->\n<!-- /step:only -->\n"
	_ = w.repairOutputWithPlan(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "t"}}, models.AgentProfile{}, plan, out)
	if gw.redoCalls["only"] != 3 {
		t.Fatalf("redo calls for step = %d, want 3", gw.redoCalls["only"])
	}
}
