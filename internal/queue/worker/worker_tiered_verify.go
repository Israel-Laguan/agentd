package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

// tieredVerifyOutcomeEvent records the classified outcome of a verify step
// on the verify task itself. The origin task reads the most recent one to
// decide whether the pipeline succeeded, so it must be emitted for every
// terminal verify run — including the ones that route into the ladder.
const tieredVerifyOutcomeEvent = "TIERED_VERIFY_OUTCOME"

// processTieredVerifyStep runs the verify step, then parses and classifies
// the VerifyResult the model produced and routes it through the escalation
// ladder. Without this interception a failing verify would commit a
// *successful* task result (the model answered, after all) and the origin
// would complete as if the checks had passed.
func (w *Worker) processTieredVerifyStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, parentTask models.Task) {
	result, ok := w.processAgentic(ctx, task, project, profile)
	if !ok {
		// Handed off, suspended, or fell back to legacy execution: there is no
		// VerifyResult to classify, so the checks did not run.
		w.failTieredStep(ctx, task, "tiered verify step produced no VerifyResult")
		w.failTieredDependents(ctx, task, parentTask)
		return
	}
	if !result.IsTerminalSuccess() {
		w.handleLoopResult(ctx, task, result)
		return
	}

	// A step may decide the sealed pack cannot answer the task at all. That
	// outranks any verdict it would otherwise report.
	if reason, ok := w.detectNeedsContext(ctx, task); ok {
		if err := w.handleNeedsContext(ctx, task, parentTask, reason); err != nil {
			slog.Error("tiered verify: re-gather failed", "task_id", task.ID, "error", err)
			w.failTieredOrigin(ctx, parentTask, "re-gather failed: "+err.Error())
		}
		return
	}

	verifyResult, err := w.readVerifyResult(ctx, task)
	if err != nil {
		// The step ran but produced no parseable verdict. Treating that as a
		// pass would let unverified work complete, so classify it as a hard
		// failure and let the ladder bound the retries.
		slog.Error("tiered verify: failed to parse VerifyResult; treating as fail",
			"task_id", task.ID, "error", err)
		w.Emit(ctx, task, "TIERED_VERIFY_PARSE_ERROR", err.Error())
		verifyResult = VerifyResult{Overall: "fail"}
	}

	outcome := ClassifyVerifyOutcome(verifyResult)
	w.Emit(ctx, task, tieredVerifyOutcomeEvent, string(outcome))
	slog.Info("tiered verify classified",
		"task_id", task.ID, "origin_id", parentTask.ID, "outcome", outcome)

	if err := w.handleVerifyOutcome(ctx, task, outcome, verifyResult, parentTask); err != nil {
		slog.Error("tiered verify: escalation ladder failed",
			"task_id", task.ID, "outcome", outcome, "error", err)
		w.Emit(ctx, task, "TIERED_ESCALATION_ERROR", err.Error())
		// The ladder could not schedule the follow-up work, so nothing will
		// unblock the origin. Resolve it now instead of leaving it BLOCKED.
		w.failTieredOrigin(ctx, parentTask, "escalation ladder failed: "+err.Error())
	}
}

// readVerifyResult reads the verify step's committed output from its RESULT
// event and parses the VerifyResult JSON out of it.
func (w *Worker) readVerifyResult(ctx context.Context, task models.Task) (VerifyResult, error) {
	payload, err := w.latestResultPayload(ctx, task)
	if err != nil {
		return VerifyResult{}, err
	}
	return parseVerifyResult(payload)
}

// parseVerifyResult extracts a VerifyResult from the model output. The
// payload carries a worker-added "exit=… duration=…" preamble and the model
// may wrap its JSON in markdown fences, so the object is located rather than
// unmarshalled from the whole string.
func parseVerifyResult(output string) (VerifyResult, error) {
	var vr VerifyResult
	candidate := extractJSONObject(output)
	if candidate == "" {
		return vr, fmt.Errorf("no JSON object found in verify output")
	}
	if err := json.Unmarshal([]byte(candidate), &vr); err != nil {
		return vr, fmt.Errorf("invalid VerifyResult JSON: %w", err)
	}
	if vr.Overall == "" && len(vr.Results) == 0 {
		return vr, fmt.Errorf("verify output has neither overall nor results")
	}
	return vr, nil
}

// extractJSONObject returns the first balanced top-level JSON object in s,
// preferring the contents of a markdown code fence when one is present.
func extractJSONObject(s string) string {
	if fenced := extractFenced(s); fenced != "" {
		if obj := balancedObject(fenced); obj != "" {
			return obj
		}
	}
	return balancedObject(s)
}

// extractFenced returns the body of the first markdown code fence in s.
func extractFenced(s string) string {
	idx := strings.Index(s, "```")
	if idx == -1 {
		return ""
	}
	rest := s[idx+3:]
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		// Drop the optional language tag on the opening fence line.
		if !strings.Contains(strings.TrimSpace(rest[:nl]), "{") {
			rest = rest[nl+1:]
		}
	}
	if end := strings.Index(rest, "```"); end != -1 {
		return rest[:end]
	}
	return rest
}

// balancedObject returns the first brace-balanced JSON object in s, ignoring
// braces that appear inside string literals.
func balancedObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
