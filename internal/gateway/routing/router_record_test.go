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
		Purpose: p, TaskID: "t1", AgentID: "a1",
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
	assertCascadeOutcomes(t, rec.recs[0], rec.recs[1])
	assertAttempt(t, rec.recs[1])
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

func TestRecordCarriesTheRequestPurpose(t *testing.T) {
	for _, want := range []spec.Purpose{spec.PurposeIntakeDraftPlan, spec.PurposeCreateNextPhase} {
		p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: onePlan}}}
		rec := &sliceRecorder{}
		if _, err := NewRouter(p).WithRecorder(rec).Generate(context.Background(), userReq(want, "build it")); err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if len(rec.recs) != 1 || rec.recs[0].Purpose != want {
			t.Errorf("%s: records = %+v, want purpose %q", want, rec.recs, want)
		}
	}
}

func assertCascadeOutcomes(t *testing.T, a, b spec.CallRecord) {
	t.Helper()
	if a.Provider != "down" || a.Outcome != spec.OutcomeFailed || a.Cascade {
		t.Errorf("first attempt = %+v, want failed on down, not cascade", a)
	}
	if b.Provider != "up" || b.Outcome != spec.OutcomeOK || !b.Cascade {
		t.Errorf("second attempt = %+v, want ok on up, marked cascade", b)
	}
}

func assertAttempt(t *testing.T, b spec.CallRecord) {
	t.Helper()
	if b.Tokens != 42 || b.CachedTokens != 3 || b.Model != "up-m" || b.InputChars != 5 {
		t.Errorf("sizes wrong: %+v", b)
	}
	if b.Purpose != spec.PurposeExecTurn || b.Stage != "exec" || b.TaskID != "t1" || b.AgentID != "a1" {
		t.Errorf("attribution wrong: %+v", b)
	}
}

// The record carries the agent the router chose: the rule or default's agent on a
// routed call, the profile on a pin, and nothing on the cascade.
func TestRecordCarriesTheResolvedAgent(t *testing.T) {
	cases := []struct {
		name    string
		purpose spec.Purpose
		agentID string
		want    string
	}{
		{"routed to an agent", spec.PurposeCreateElicit, "default", "quick"},
		{"pinned to a profile", spec.PurposeCreateElicit, "tier-fast", "tier-fast"},
		{"cascade", spec.PurposeExecWorkPlan, "default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			openai, gemini := purposeProviders()
			rec := &sliceRecorder{}
			router := NewRouter(openai, gemini).WithPurposeRouting(purposeTestTable()).WithRecorder(rec)
			_, err := router.Generate(context.Background(), spec.AIRequest{
				Purpose: tc.purpose, AgentID: tc.agentID, TaskID: "t1",
				Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(rec.recs) != 1 || rec.recs[0].RoutedAgent != tc.want {
				t.Fatalf("records = %+v, want one with RoutedAgent %q", rec.recs, tc.want)
			}
		})
	}
}
