package runtime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestTurnLoopRunner_ReportReturnsResult(t *testing.T) {
	t.Parallel()
	runner := NewTurnLoopRunner()
	want := LoopResult{Status: LoopSuccessfulCompletion}
	var recorded []LoopResult

	got, err := runner.Run(context.Background(), Request{
		TaskID: "task-1",
		Iterate: func(context.Context, string, int, *IterationState) (IterationOutcome, error) {
			return IterationOutcome{Result: want, Report: true}, nil
		},
		RecordResult: func(r LoopResult) {
			recorded = append(recorded, r)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Reported || got.LoopResult.Status != LoopSuccessfulCompletion {
		t.Fatalf("result = %+v, want reported success", got)
	}
	if len(recorded) != 1 || recorded[0].Status != LoopSuccessfulCompletion {
		t.Fatalf("recorded = %+v, want one success result", recorded)
	}
}

func TestTurnLoopRunner_RewindResetsStateAndContinues(t *testing.T) {
	t.Parallel()
	runner := NewTurnLoopRunner()
	var calls int
	var resetCount int
	var turnIDs []string

	got, err := runner.Run(context.Background(), Request{
		TaskID: "task-1",
		Iterate: func(_ context.Context, turnID string, turnIndex int, _ *IterationState) (IterationOutcome, error) {
			calls++
			turnIDs = append(turnIDs, turnID)
			if calls == 1 {
				return IterationOutcome{Continue: true, RewindTo: RewindToFirstTurn}, nil
			}
			if turnIndex != 0 {
				t.Fatalf("turnIndex after rewind = %d, want 0", turnIndex)
			}
			return IterationOutcome{Result: LoopResult{Status: LoopSuccessfulCompletion}, Report: true}, nil
		},
		ResetForRewind: func() {
			resetCount++
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Reported {
		t.Fatal("expected reported result")
	}
	if resetCount != 1 {
		t.Fatalf("resetCount = %d, want 1", resetCount)
	}
	if len(turnIDs) != 2 || turnIDs[0] != "task-1:0" || turnIDs[1] != "task-1:0" {
		t.Fatalf("turnIDs = %v, want repeated turn 0", turnIDs)
	}
}

func TestTurnLoopRunner_TopicDriftResetUsesTopicReset(t *testing.T) {
	t.Parallel()
	runner := NewTurnLoopRunner()
	var calls int
	var topicResets int

	got, err := runner.Run(context.Background(), Request{
		TaskID: "task-1",
		Iterate: func(context.Context, string, int, *IterationState) (IterationOutcome, error) {
			calls++
			if calls == 1 {
				return IterationOutcome{Continue: true, RewindTo: RewindToFirstTurn}, ErrTopicDriftReset
			}
			return IterationOutcome{Result: LoopResult{Status: LoopSuccessfulCompletion}, Report: true}, nil
		},
		ResetForTopicDrift: func() {
			topicResets++
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Reported {
		t.Fatal("expected reported result")
	}
	if topicResets != 1 {
		t.Fatalf("topicResets = %d, want 1", topicResets)
	}
}

func TestTurnLoopRunner_RewindStagnationReportsTypedResult(t *testing.T) {
	t.Parallel()
	runner := NewTurnLoopRunner()
	var resets int
	var recorded []LoopResult
	custom := LoopResult{Status: LoopTurnLimitExceeded, Meta: LoopMeta{LastError: "custom stagnation"}}

	got, err := runner.Run(context.Background(), Request{
		TaskID: "task-1",
		Iterate: func(context.Context, string, int, *IterationState) (IterationOutcome, error) {
			return IterationOutcome{Continue: true, RewindTo: RewindToFirstTurn}, nil
		},
		ResetForRewind: func() {
			resets++
		},
		RecordResult: func(r LoopResult) {
			recorded = append(recorded, r)
		},
		BuildRewindStagnationResult: func(turnIndex int) LoopResult {
			if turnIndex != 0 {
				t.Fatalf("turnIndex = %d, want 0", turnIndex)
			}
			return custom
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !got.Reported || got.LoopResult.Meta.LastError != "custom stagnation" {
		t.Fatalf("result = %+v, want custom stagnation", got)
	}
	if resets != MaxRewindStreak {
		t.Fatalf("resets = %d, want %d", resets, MaxRewindStreak)
	}
	if len(recorded) != 1 || recorded[0].Meta.LastError != "custom stagnation" {
		t.Fatalf("recorded = %+v, want custom stagnation", recorded)
	}
}

func TestTurnLoopRunner_PropagatesNonRuntimeError(t *testing.T) {
	t.Parallel()
	runner := NewTurnLoopRunner()
	wantErr := errors.New("gateway failed")

	_, err := runner.Run(context.Background(), Request{
		TaskID: "task-1",
		Iterate: func(context.Context, string, int, *IterationState) (IterationOutcome, error) {
			return IterationOutcome{}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
}

// TestTurnLoopRunner_LogsTurnLifecycle verifies the debug lifecycle events
// (turn start/end) carry task/turn correlation fields.
func TestTurnLoopRunner_LogsTurnLifecycle(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	runner := NewTurnLoopRunner()
	_, err := runner.Run(context.Background(), Request{
		TaskID: "task-lifecycle-1",
		Iterate: func(_ context.Context, _ string, turnIndex int, _ *IterationState) (IterationOutcome, error) {
			if turnIndex == 0 {
				return IterationOutcome{Continue: true, RewindTo: RewindNone}, nil
			}
			return IterationOutcome{Result: LoopResult{Status: LoopSuccessfulCompletion}, Report: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	logs := buf.String()
	for _, event := range []string{"agentic: turn start", "agentic: turn end"} {
		if !strings.Contains(logs, event) {
			t.Errorf("lifecycle event %q missing; logs = %q", event, logs)
		}
	}
	if !strings.Contains(logs, "task_id=task-lifecycle-1") {
		t.Errorf("task_id field missing; logs = %q", logs)
	}
	if !strings.Contains(logs, "turn_id=task-lifecycle-1:0") {
		t.Errorf("turn_id field missing; logs = %q", logs)
	}
}
