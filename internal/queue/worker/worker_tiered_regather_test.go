package worker

import (
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestParseNeedsContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		output     string
		wantFound  bool
		wantReason string
	}{
		{
			name:       "explicit signal",
			output:     `{"needs_context": true, "reason": "handlers/ is not in the pack"}`,
			wantFound:  true,
			wantReason: "handlers/ is not in the pack",
		},
		{
			name:       "with worker preamble",
			output:     "exit=0 duration=3s\n{\"needs_context\":true,\"reason\":\"scope wrong\"}",
			wantFound:  true,
			wantReason: "scope wrong",
		},
		{
			name:       "fenced",
			output:     "```json\n{\"needs_context\":true}\n```",
			wantFound:  true,
			wantReason: "step reported the ContextPack was insufficient",
		},
		{name: "explicitly false", output: `{"needs_context": false}`},
		{name: "a normal verify result", output: `{"results":[],"overall":"pass"}`},
		{name: "a decision artifact", output: `{"touch_list":["a.go"],"checks":["go test ./..."]}`},
		{name: "no json at all", output: "I could not find the files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason, found := parseNeedsContext(tt.output)
			if found != tt.wantFound {
				t.Fatalf("parseNeedsContext(%q) found = %v, want %v", tt.output, found, tt.wantFound)
			}
			if found && reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestHandleNeedsContext_AppendsFreshChainAndAbandonsStaleSteps(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	decision := f.step(t, TieredStepDecision)

	// Decision runs and reports the pack is unusable. Context is done;
	// execute and verify are still PENDING behind it.
	f.completeSteps(t, f.step(t, TieredStepContext))

	if err := f.worker.handleNeedsContext(f.ctx, decision, f.origin, "handlers/ missing from pack"); err != nil {
		t.Fatalf("handleNeedsContext: %v", err)
	}

	steps := f.tieredChildren(t)
	if len(steps) != 8 {
		t.Fatalf("steps after re-gather = %d, want 8 (a fresh 4-step chain appended)", len(steps))
	}
	if got := countTieredSteps(steps, TieredStepContext); got != 2 {
		t.Errorf("context steps = %d, want 2 (the re-gather pays for a new pack)", got)
	}

	// The stale downstream steps must not run against the old pack.
	abandoned := 0
	for _, s := range steps {
		if s.State == models.TaskStateNeedsContext {
			abandoned++
		}
	}
	if abandoned != 2 {
		t.Errorf("abandoned steps = %d, want 2 (the stale execute and verify)", abandoned)
	}

	// A re-gather is an explicit board action, so the origin waits on it.
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateBlocked {
		t.Errorf("origin state = %s, want %s", state, models.TaskStateBlocked)
	}

	// The reason has to reach the new context step or the re-gather repeats.
	var fresh *models.Task
	for i, s := range steps {
		if tieredStepProfiles[s.AgentID] == TieredStepContext && s.ID != f.step(t, TieredStepContext).ID {
			fresh = &steps[i]
		}
	}
	if fresh == nil {
		t.Fatal("no new context step found")
	}
	if !strings.Contains(fresh.Description, "handlers/ missing from pack") {
		t.Errorf("new context step lost the re-gather reason: %q", fresh.Description)
	}
	if fresh.State != models.TaskStateReady {
		t.Errorf("new context step state = %s, want %s", fresh.State, models.TaskStateReady)
	}
}

func TestTryResolveTieredOrigin_IgnoresAbandonedSteps(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	decision := f.step(t, TieredStepDecision)
	f.completeSteps(t, f.step(t, TieredStepContext))

	if err := f.worker.handleNeedsContext(f.ctx, decision, f.origin, "pack too narrow"); err != nil {
		t.Fatalf("handleNeedsContext: %v", err)
	}
	// Resolve the whole board: the re-gather chain passes, the abandoned
	// steps stay NEEDS_CONTEXT.
	var newVerify *models.Task
	steps := f.tieredChildren(t)
	for i, s := range steps {
		if s.State == models.TaskStateNeedsContext {
			continue
		}
		if tieredStepProfiles[s.AgentID] == TieredStepVerify {
			newVerify = &steps[i]
		}
	}
	if newVerify == nil {
		t.Fatal("no live verify step after re-gather")
	}
	for _, s := range steps {
		if s.State == models.TaskStateNeedsContext || s.State == models.TaskStateCompleted {
			continue
		}
		f.completeSteps(t, s)
	}
	f.worker.Emit(f.ctx, *newVerify, tieredVerifyOutcomeEvent, string(models.VerifyOutcomePass))

	origin := f.reload(t, f.origin.ID)
	if !f.worker.tryResolveTieredOrigin(f.ctx, origin) {
		t.Fatal("tryResolveTieredOrigin = false, want true")
	}
	// Abandoned steps are not pending work; the pipeline must not deadlock.
	if state := f.reload(t, f.origin.ID).State; state != models.TaskStateCompleted {
		t.Errorf("origin state = %s, want %s (abandoned steps must not block resolution)",
			state, models.TaskStateCompleted)
	}
}

func TestTryResolveTieredOrigin_SkipsVerifyWithoutVerdict(t *testing.T) {
	t.Parallel()
	f := newTieredFixture(t)
	oldVerify := f.step(t, TieredStepVerify)
	f.completeSteps(t, f.steps...)
	f.worker.Emit(f.ctx, oldVerify, tieredVerifyOutcomeEvent, string(models.VerifyOutcomePass))

	// Append a redo whose verify never recorded a verdict (e.g. it was
	// abandoned). The older, real verdict must still be found.
	if err := f.worker.scheduleEscalation(f.ctx, f.origin, VerifyResult{Overall: "fail"}); err != nil {
		t.Fatalf("scheduleEscalation: %v", err)
	}
	for _, s := range f.tieredChildren(t) {
		if s.State == models.TaskStateCompleted {
			continue
		}
		if _, err := f.store.UpdateTaskState(f.ctx, s.ID, s.UpdatedAt, models.TaskStateNeedsContext); err != nil {
			t.Fatalf("park step: %v", err)
		}
	}

	outcome, found := f.worker.latestVerifyOutcome(f.ctx, f.tieredChildren(t))
	if !found {
		t.Fatal("latestVerifyOutcome found = false; a recorded verdict was skipped")
	}
	if outcome != models.VerifyOutcomePass {
		t.Errorf("outcome = %v, want %v", outcome, models.VerifyOutcomePass)
	}
}
