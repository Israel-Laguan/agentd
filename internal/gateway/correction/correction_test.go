package correction_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type gwStep struct {
	content string
	err     error
}

type seqFakeGW struct {
	steps []gwStep
	i     int
}

func (g *seqFakeGW) Generate(_ context.Context, _ spec.AIRequest) (spec.AIResponse, error) {
	if g.i >= len(g.steps) {
		return spec.AIResponse{}, errors.New("exhausted gateway sequence")
	}
	s := g.steps[g.i]
	g.i++
	return spec.AIResponse{Content: s.content}, s.err
}

func (g *seqFakeGW) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return nil, errors.New("not used")
}

func (g *seqFakeGW) AnalyzeScope(context.Context, string) (*spec.ScopeAnalysis, error) {
	return nil, errors.New("not used")
}

func (g *seqFakeGW) ClassifyIntent(context.Context, string) (*spec.IntentAnalysis, error) {
	return nil, errors.New("not used")
}

type simplePayload struct {
	Name string `json:"name"`
}

type validPayload struct {
	N int `json:"n"`
}

func (v *validPayload) Validate() error {
	if v.N < 1 {
		return errors.New("n must be positive")
	}
	return nil
}

func TestGenerateJSONSuccessFirstAttempt(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{{content: `{"name":"alice"}`}}}
	out, err := correction.GenerateJSON[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatalf("GenerateJSON: %v", err)
	}
	if out.Name != "alice" {
		t.Fatalf("Name = %q", out.Name)
	}
}

func TestGenerateJSONRetryAfterInvalidJSON(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{
		{content: `not-json`},
		{content: `{"name":"fixed"}`},
	}}
	out, err := correction.GenerateJSON[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatalf("GenerateJSON: %v", err)
	}
	if out.Name != "fixed" {
		t.Fatalf("Name = %q", out.Name)
	}
	if gw.i != 2 {
		t.Fatalf("gateway calls = %d, want 2", gw.i)
	}
}

func TestGenerateJSONRetryAfterValidationError(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{
		{content: `{"n":0}`},
		{content: `{"n":3}`},
	}}
	out, err := correction.GenerateJSON[validPayload](context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatalf("GenerateJSON: %v", err)
	}
	if out.N != 3 {
		t.Fatalf("N = %d", out.N)
	}
}

func TestGenerateJSONGatewayError(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{{content: "", err: errors.New("unavailable")}}}
	_, err := correction.GenerateJSON[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err == nil || err.Error() != "unavailable" {
		t.Fatalf("expected unavailable, got %v", err)
	}
}

func TestGenerateJSONExhaustedInvalidJSON(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{
		{content: `bad`},
		{content: `still-bad`},
		{content: `nope`},
	}}
	_, err := correction.GenerateJSON[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err == nil || !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("expected ErrInvalidJSONResponse wrap, got %v", err)
	}
}

func TestSummarizeRaw(t *testing.T) {
	if got := correction.SummarizeRaw("  hi  "); got != "hi" {
		t.Fatalf("trim: %q", got)
	}
	long := strings.Repeat("a", 1200)
	got := correction.SummarizeRaw(long)
	if len(got) <= 1000 || !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("truncation: len=%d suffix=%q", len(got), got[len(got)-20:])
	}
}

func TestEnforcePhaseCap(t *testing.T) {
	tasks := []models.DraftTask{
		{Title: "a"}, {Title: "b"}, {Title: "c"}, {Title: "d"},
	}
	plan := models.DraftPlan{Tasks: tasks}

	out := correction.EnforcePhaseCap(plan, 0)
	if len(out.Tasks) != 4 {
		t.Fatalf("max<=0 should not trim, got %d", len(out.Tasks))
	}

	out = correction.EnforcePhaseCap(plan, 10)
	if len(out.Tasks) != 4 {
		t.Fatalf("under cap: %d", len(out.Tasks))
	}

	out = correction.EnforcePhaseCap(plan, 1)
	if len(out.Tasks) != 1 || !strings.Contains(out.Tasks[0].Title, "Phase") {
		t.Fatalf("cap=1: %#v", out.Tasks)
	}

	out = correction.EnforcePhaseCap(plan, 2)
	if len(out.Tasks) != 2 {
		t.Fatalf("cap=2 len=%d", len(out.Tasks))
	}
}

func TestPromptAfterInvalidJSON(t *testing.T) {
	msg := correction.PromptAfterInvalidJSON(errors.New("eof"))
	if msg.Role != "user" || !strings.Contains(msg.Content, "eof") {
		t.Fatalf("msg = %#v", msg)
	}
}
