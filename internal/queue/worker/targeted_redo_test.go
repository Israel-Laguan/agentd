package worker

import (
	"context"
	"fmt"
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

func TestReplaceSection_MissingCloseTag(t *testing.T) {
	t.Parallel()
	out := "<!-- step:x -->\nold body without close\n"
	replaced := replaceSection(out, "x", "fixed")
	body, ok := extractSection(replaced, "x")
	if !ok || body != "fixed" {
		t.Fatalf("replace missing close: body=%q ok=%v", body, ok)
	}
}

func TestReplaceSection_MissingCloseTag_PreservesLaterSections(t *testing.T) {
	t.Parallel()
	out := "<!-- step:x -->\nbroken\n<!-- step:y -->\ntail\n<!-- /step:y -->\n"
	replaced := replaceSection(out, "x", "fixed")
	body, ok := extractSection(replaced, "x")
	if !ok || body != "fixed" {
		t.Fatalf("replace x: body=%q ok=%v", body, ok)
	}
	bodyY, ok := extractSection(replaced, "y")
	if !ok || bodyY != "tail" {
		t.Fatalf("replace preserved y: body=%q ok=%v", bodyY, ok)
	}
}

func TestValidateOutput_ListFormats(t *testing.T) {
	t.Parallel()
	validCases := []struct {
		id   string
		body string
	}{
		{"ordered", "1. first\n2. second"},
		{"nested", "  - nested item"},
	}
	for _, tc := range validCases {
		plan := Plan{Steps: []PlanStep{{ID: tc.id, OutputFormat: "list"}}}
		out := fmt.Sprintf("<!-- step:%s -->\n%s\n<!-- /step:%s -->\n", tc.id, tc.body, tc.id)
		if fail := ValidateOutput(out, plan); len(fail) != 0 {
			t.Fatalf("%s list: failing %v", tc.id, fail)
		}
	}
	plan := Plan{Steps: []PlanStep{{ID: "plain", OutputFormat: "list"}}}
	plain := `<!-- step:plain -->
not a list
<!-- /step:plain -->
`
	if fail := ValidateOutput(plain, plan); len(fail) != 1 || fail[0].ID != "plain" {
		t.Fatalf("plain text list fail = %v", fail)
	}
}

func TestFormatPlanOutputForCommit(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []PlanStep{
		{ID: "analyze", OutputFormat: "text"},
		{ID: "summarize", OutputFormat: "text"},
	}}
	marked := `<!-- step:analyze -->
first body
<!-- /step:analyze -->
<!-- step:summarize -->
second body
<!-- /step:summarize -->
`
	got := formatPlanOutputForCommit(marked, plan)
	if strings.Contains(got, "<!-- step:") {
		t.Fatalf("expected no step markers, got %q", got)
	}
	want := "first body\n\nsecond body"
	if got != want {
		t.Fatalf("formatPlanOutputForCommit() = %q, want %q", got, want)
	}
	plain := "unmarked output"
	if formatPlanOutputForCommit(plain, plan) != plain {
		t.Fatalf("unmarked output should be returned unchanged")
	}
}

func TestFormatPlanOutputForCommit_EmptyMarkedSections(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []PlanStep{{ID: "a", OutputFormat: "text"}}}
	marked := "<!-- step:a -->\n<!-- /step:a -->\n"
	got := formatPlanOutputForCommit(marked, plan)
	if strings.Contains(got, "<!-- step:") {
		t.Fatalf("expected no step markers, got %q", got)
	}
	if got != "" {
		t.Fatalf("formatPlanOutputForCommit() = %q, want empty string", got)
	}
}

func TestPreparePlanCommitContent_ValidJoins(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []PlanStep{
		{ID: "analyze", OutputFormat: "text"},
		{ID: "summarize", OutputFormat: "text"},
	}}
	marked := `<!-- step:analyze -->
first body
<!-- /step:analyze -->
<!-- step:summarize -->
second body
<!-- /step:summarize -->
`
	got := preparePlanCommitContent(marked, plan)
	if strings.Contains(got, "<!-- step:") {
		t.Fatalf("expected no step markers, got %q", got)
	}
	want := "first body\n\nsecond body"
	if got != want {
		t.Fatalf("preparePlanCommitContent() = %q, want %q", got, want)
	}
}

func TestPreparePlanCommitContent_InvalidStripsNotJoins(t *testing.T) {
	t.Parallel()
	plan := Plan{Steps: []PlanStep{
		{ID: "a", OutputFormat: "text"},
		{ID: "b", OutputFormat: "text"},
	}}
	marked := `<!-- step:a -->
good body
<!-- /step:a -->
extra trailing text
`
	got := preparePlanCommitContent(marked, plan)
	joined := formatPlanOutputForCommit(marked, plan)
	if got == joined {
		t.Fatalf("invalid plan should not use partial join: got %q, joined %q", got, joined)
	}
	if !strings.Contains(got, "good body") || !strings.Contains(got, "extra trailing text") {
		t.Fatalf("stripped in-situ output = %q, want both body and trailing text", got)
	}
	if strings.Contains(got, "<!-- step:") {
		t.Fatalf("expected no step markers, got %q", got)
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
	_ = w.repairOutputWithPlan(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "t"}}, plan, out, nil)
	if gw.redoCalls["only"] != 3 {
		t.Fatalf("redo calls for step = %d, want 3", gw.redoCalls["only"])
	}
}
