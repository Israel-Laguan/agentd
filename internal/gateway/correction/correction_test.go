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
	model   string // answering model reported in UsageDetails, when set
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
	resp := spec.AIResponse{Content: s.content}
	if s.model != "" {
		resp.UsageDetails = &spec.UsageDetails{Model: s.model}
	}
	return resp, s.err
}

func (g *seqFakeGW) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.NoopEmbed(ctx, req)
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

func TestPromptAfterInvalidJSON(t *testing.T) {
	msg := correction.PromptAfterInvalidJSON(errors.New("eof"))
	if msg.Role != "user" || !strings.Contains(msg.Content, "eof") {
		t.Fatalf("msg = %#v", msg)
	}
}

type hintedPayload struct {
	N int `json:"n"`
}

func (hintedPayload) SchemaHint() string { return "SCHEMA-HINT-XYZ" }

func TestGenerateJSONCorrectionIncludesSchemaHint(t *testing.T) {
	var seen []spec.AIRequest
	gw := &recordingGW{seen: &seen, steps: []string{`not-json`, `{"n":1}`}}
	if _, err := correction.GenerateJSON[hintedPayload](context.Background(), gw, spec.AIRequest{}); err != nil {
		t.Fatalf("GenerateJSON: %v", err)
	}
	last := seen[1].Messages[len(seen[1].Messages)-1].Content
	if !strings.Contains(last, "SCHEMA-HINT-XYZ") {
		t.Fatalf("correction message = %q, want schema hint", last)
	}
}

type recordingGW struct {
	seqFakeGW
	seen  *[]spec.AIRequest
	steps []string
	n     int
}

func (g *recordingGW) Generate(_ context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	*g.seen = append(*g.seen, req)
	c := g.steps[g.n]
	g.n++
	return spec.AIResponse{Content: c}, nil
}

// B-054: the usage details of a JSON call name the model that produced the
// accepted answer, so TOKEN_USAGE can record it after correction retries.
func TestGenerateJSONWithUsageKeepsAnsweringModel(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{
		{content: `not json`, model: "poolside/laguna-m.1"},
		{content: `{"name":"alice"}`, model: "poolside/laguna-s-2.1"},
	}}
	_, _, details, err := correction.GenerateJSONWithUsage[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatalf("GenerateJSONWithUsage: %v", err)
	}
	if details.Model != "poolside/laguna-s-2.1" {
		t.Fatalf("details.Model = %q, want the model of the accepted answer", details.Model)
	}
}

// B-054: when the accepted answer reports no model, details.Model is empty
// rather than the model of an earlier, rejected answer.
func TestGenerateJSONWithUsageDropsRejectedAnswerModel(t *testing.T) {
	gw := &seqFakeGW{steps: []gwStep{
		{content: `not json`, model: "poolside/laguna-m.1"},
		{content: `{"name":"alice"}`},
	}}
	_, _, details, err := correction.GenerateJSONWithUsage[simplePayload](context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatalf("GenerateJSONWithUsage: %v", err)
	}
	if details.Model != "" {
		t.Fatalf("details.Model = %q, want empty: the accepted answer reported no model", details.Model)
	}
}

type emptyHintPayload struct {
	N int `json:"n"`
}

func (emptyHintPayload) SchemaHint() string { return "" }

// An empty hint adds nothing to the corrective message, not even a newline.
func TestGenerateJSONCorrectionOmitsEmptySchemaHint(t *testing.T) {
	var seen []spec.AIRequest
	gw := &recordingGW{seen: &seen, steps: []string{`not-json`, `{"n":1}`}}
	if _, err := correction.GenerateJSON[emptyHintPayload](context.Background(), gw, spec.AIRequest{}); err != nil {
		t.Fatalf("GenerateJSON: %v", err)
	}
	last := seen[1].Messages[len(seen[1].Messages)-1].Content
	if strings.HasSuffix(last, "\n") {
		t.Fatalf("correction message = %q, want no trailing newline", last)
	}
}
