package routing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"agentd/internal/api/correlation"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

type scriptedProvider struct {
	name    string
	replies []scriptedReply
	calls   int
}

type scriptedReply struct {
	content string
	err     error
	tokens  int
}

func (p *scriptedProvider) Name() spec.Provider { return spec.Provider(p.name) }
func (p *scriptedProvider) MaxInputChars() int  { return 0 }
func (p *scriptedProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (p *scriptedProvider) Generate(_ context.Context, _ spec.AIRequest) (spec.AIResponse, error) {
	r := p.replies[p.calls]
	p.calls++
	if r.err != nil {
		return spec.AIResponse{}, r.err
	}
	return spec.AIResponse{
		Content: r.content, TokenUsage: r.tokens, ProviderUsed: p.name, ModelUsed: p.name + "-m",
		UsageDetails: &spec.UsageDetails{CachedTokens: 3},
	}, nil
}

type sliceRecorder struct {
	mu   sync.Mutex
	recs []spec.CallRecord
}

func (s *sliceRecorder) Record(r spec.CallRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, r)
}

func userReq(p spec.Purpose, content string) spec.AIRequest {
	return spec.AIRequest{
		Purpose: p, TaskID: "t1", AgentID: "a1", Role: spec.RoleWorker,
		Messages: []spec.PromptMessage{{Role: "user", Content: content}},
	}
}

func TestEachProviderAttemptGetsItsOwnRecord(t *testing.T) {
	down := &scriptedProvider{name: "down", replies: []scriptedReply{{err: errors.New("boom")}}}
	up := &scriptedProvider{name: "up", replies: []scriptedReply{{content: "hi", tokens: 42}}}
	rec := &sliceRecorder{}
	r := NewRouter(down, up).WithRecorder(rec)
	if _, err := r.Generate(context.Background(), userReq(spec.PurposeExecTurn, "hello")); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 2 {
		t.Fatalf("got %d records, want 2 (one per provider attempt)", len(rec.recs))
	}
	a, b := rec.recs[0], rec.recs[1]
	if a.Provider != "down" || a.Outcome != spec.OutcomeFailed || a.Cascade {
		t.Errorf("first attempt = %+v, want failed on down, not cascade", a)
	}
	if b.Provider != "up" || b.Outcome != spec.OutcomeOK || !b.Cascade {
		t.Errorf("second attempt = %+v, want ok on up, marked cascade", b)
	}
	if b.Tokens != 42 || b.CachedTokens != 3 || b.Model != "up-m" || b.InputChars != 5 {
		t.Errorf("sizes wrong: %+v", b)
	}
	if b.Purpose != spec.PurposeExecTurn || b.Stage != "exec" || b.TaskID != "t1" || b.AgentID != "a1" {
		t.Errorf("attribution wrong: %+v", b)
	}
}

func TestEmptyPurposeIsRecordedAsUnlabelled(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	rec := &sliceRecorder{}
	r := NewRouter(p).WithRecorder(rec)
	if _, err := r.Generate(context.Background(), userReq("", "hi")); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 1 || rec.recs[0].Purpose != spec.PurposeUnlabelled || rec.recs[0].Stage != "unlabelled" {
		t.Fatalf("records = %+v, want one unlabelled record", rec.recs)
	}
}

func TestJSONSyntaxRetriesAreRecordedAsRepairs(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "not json"}, {content: `{"a":1}`}}}
	rec := &sliceRecorder{}
	r := NewRouter(p).WithRecorder(rec)
	req := userReq(spec.PurposeIntakeClassifyIntent, "hi")
	req.JSONMode = true
	if _, err := r.Generate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 2 {
		t.Fatalf("got %d records, want 2", len(rec.recs))
	}
	if rec.recs[0].Attempt != spec.AttemptFirst || rec.recs[0].Outcome != spec.OutcomeInvalidJSON {
		t.Errorf("first = %+v, want first/invalid_json", rec.recs[0])
	}
	if rec.recs[1].Attempt != spec.AttemptJSONRepair || rec.recs[1].Outcome != spec.OutcomeOK {
		t.Errorf("second = %+v, want json_repair/ok", rec.recs[1])
	}
}

func TestRepairMarkerFromACallerLayerSurvives(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	rec := &sliceRecorder{}
	req := userReq(spec.PurposeExecWorkPlan, "hi")
	req.Attempt = spec.AttemptJSONRepair
	if _, err := NewRouter(p).WithRecorder(rec).Generate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if rec.recs[0].Attempt != spec.AttemptJSONRepair {
		t.Fatalf("attempt = %q, want json_repair set by the caller's retry loop", rec.recs[0].Attempt)
	}
}

func TestTruncationIsFlaggedOnTheRecord(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	rec := &sliceRecorder{}
	r := NewRouter(p).WithRecorder(rec).
		WithTruncation(truncation.StrategyTruncator{Strategy: truncation.MiddleOutStrategy{}}, 20)
	if _, err := r.Generate(context.Background(), userReq(spec.PurposeExecBatch, strings.Repeat("a", 100))); err != nil {
		t.Fatal(err)
	}
	if !rec.recs[0].Truncated || rec.recs[0].InputChars > 20 {
		t.Fatalf("record = %+v, want truncated with InputChars <= 20", rec.recs[0])
	}
}

func TestCorrelationIDComesFromTheRequestContext(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	rec := &sliceRecorder{}
	r := NewRouter(p).WithRecorder(rec)
	if _, err := r.Generate(correlation.WithID(context.Background(), "corr-1"), userReq(spec.PurposeIntakeDraftPlan, "hi")); err != nil {
		t.Fatal(err)
	}
	if rec.recs[0].CorrelationID != "corr-1" {
		t.Fatalf("correlation = %q", rec.recs[0].CorrelationID)
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "x"}}}
	if _, err := NewRouter(p).Generate(context.Background(), userReq(spec.PurposeExecTurn, "hi")); err != nil {
		t.Fatal(err)
	}
}

type embedProvider struct {
	scriptedProvider
	err error
}

func (p *embedProvider) Embed(_ context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	if p.err != nil {
		return spec.EmbedResponse{}, p.err
	}
	return spec.EmbedResponse{Vectors: make([][]float32, len(req.Input)), ProviderUsed: p.name}, nil
}

func TestEmbedAttemptsAreRecordedWithPurpose(t *testing.T) {
	bad := &embedProvider{scriptedProvider: scriptedProvider{name: "bad"}, err: errors.New("down")}
	good := &embedProvider{scriptedProvider: scriptedProvider{name: "good"}}
	rec := &sliceRecorder{}
	r := NewRouter(bad, good).WithRecorder(rec)
	_, err := r.Embed(context.Background(), spec.EmbedRequest{Purpose: spec.PurposeOverheadEmbedFiles, Input: []string{"ab", "cde"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 2 {
		t.Fatalf("got %d embed records, want 2", len(rec.recs))
	}
	for _, got := range rec.recs {
		if got.Kind != spec.KindEmbed || got.Purpose != spec.PurposeOverheadEmbedFiles || got.InputChars != 5 {
			t.Errorf("record = %+v", got)
		}
	}
	if rec.recs[0].Outcome != spec.OutcomeFailed || rec.recs[1].Outcome != spec.OutcomeOK || !rec.recs[1].Cascade {
		t.Errorf("outcomes = %q/%q cascade=%v", rec.recs[0].Outcome, rec.recs[1].Outcome, rec.recs[1].Cascade)
	}
}

const onePlan = `{"project_name":"p","description":"d","tasks":[{"title":"t","description":"d"}]}`

func TestGeneratePlanTakesItsPurposeFromContext(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want spec.Purpose
	}{
		{"default is a chat plan", context.Background(), spec.PurposeIntakeDraftPlan},
		{"next phase", spec.WithPurpose(context.Background(), spec.PurposeCreateNextPhase), spec.PurposeCreateNextPhase},
	}
	for _, tc := range cases {
		p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: onePlan}}}
		rec := &sliceRecorder{}
		if _, err := NewRouter(p).WithRecorder(rec).GeneratePlan(tc.ctx, "build it"); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(rec.recs) != 1 || rec.recs[0].Purpose != tc.want {
			t.Errorf("%s: records = %+v, want purpose %q", tc.name, rec.recs, tc.want)
		}
	}
}

func TestGenericStructuredJSONRetryIsMarkedAsRepair(t *testing.T) {
	// Valid JSON of the wrong shape passes the router's syntax check, so only the
	// adapter loop above it retries; it must mark its own retry.
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: `{"a":"x"}`}, {content: `{"a":1}`}}}
	rec := &sliceRecorder{}
	var out map[string]int
	if err := NewRouter(p).WithRecorder(rec).GenerateStructuredJSON(context.Background(), "go", &out); err != nil {
		t.Fatal(err)
	}
	if len(rec.recs) != 2 || rec.recs[0].Attempt != spec.AttemptFirst || rec.recs[1].Attempt != spec.AttemptJSONRepair {
		t.Fatalf("records = %+v, want [first json_repair]", rec.recs)
	}
	if rec.recs[1].Purpose != spec.PurposeLegacyContractJSON {
		t.Errorf("purpose = %q", rec.recs[1].Purpose)
	}
}
