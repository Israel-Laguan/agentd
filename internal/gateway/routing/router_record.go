package routing

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"agentd/internal/api/correlation"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

// WithRecorder sets where per-attempt call records go. Nil falls back to a
// debug log line, so a record is never silently lost.
func (r *Router) WithRecorder(rec spec.Recorder) *Router {
	if rec == nil {
		return r
	}
	r.recorder = rec
	return r
}

func (r *Router) emit(ctx context.Context, rec spec.CallRecord) {
	rec.Purpose = rec.Purpose.Normalize()
	rec.Stage = rec.Purpose.Stage()
	if rec.Attempt == "" {
		rec.Attempt = spec.AttemptFirst
	}
	rec.CorrelationID = correlation.FromContext(ctx)
	if r.recorder == nil {
		slog.DebugContext(ctx, "ai call", "purpose", rec.Purpose, "provider", rec.Provider,
			"outcome", rec.Outcome, "tokens", rec.Tokens, "latency_ms", rec.Latency.Milliseconds())
		return
	}
	r.recorder.Record(rec)
}

// attemptRecord builds the record for one provider.Generate attempt.
func attemptRecord(p providers.Backend, req spec.AIRequest, inputBefore int, started time.Time, resp spec.AIResponse, err error, cascade bool) spec.CallRecord {
	rec := spec.CallRecord{
		At: started, Kind: spec.KindGenerate, Purpose: req.Purpose, Role: req.Role,
		TaskID: req.TaskID, AgentID: req.AgentID, Provider: string(p.Name()), Model: req.Model,
		Attempt: req.Attempt, Cascade: cascade, Outcome: spec.OutcomeFailed,
		MessageCount: len(req.Messages), ToolCount: len(req.Tools),
		InputChars: messageChars(req.Messages), Latency: time.Since(started),
	}
	rec.Truncated = rec.InputChars < inputBefore
	if err != nil {
		return rec
	}
	rec.Outcome = spec.OutcomeOK
	if req.JSONMode && len(resp.ToolCalls) == 0 && !json.Valid([]byte(resp.Content)) {
		rec.Outcome = spec.OutcomeInvalidJSON
	}
	rec.OutputChars, rec.Tokens = len(resp.Content), resp.TokenUsage
	if resp.ModelUsed != "" {
		rec.Model = resp.ModelUsed
	}
	if d := resp.UsageDetails; d != nil {
		rec.CachedTokens, rec.CacheWrite = d.CachedTokens, d.CacheWriteTokens
	}
	return rec
}

func messageChars(msgs []spec.PromptMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// embedRecord builds the record for one embedding attempt.
func embedRecord(p providers.Backend, req spec.EmbedRequest, started time.Time, err error, cascade bool) spec.CallRecord {
	rec := spec.CallRecord{
		At: started, Kind: spec.KindEmbed, Purpose: req.Purpose, Provider: string(p.Name()),
		Model: req.Model, Attempt: spec.AttemptFirst, Cascade: cascade, Outcome: spec.OutcomeOK,
		Latency: time.Since(started),
	}
	for _, in := range req.Input {
		rec.InputChars += len(in)
	}
	if err != nil {
		rec.Outcome = spec.OutcomeFailed
	}
	return rec
}
