package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestProgressRatio_NoCriteria(t *testing.T) {
	g := AgentGoal{}
	if got := g.ProgressRatio(); got != 0 {
		t.Fatalf("ProgressRatio() = %v, want 0", got)
	}
}

func TestProgressRatio_AllCompleted(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a", "b"},
	}
	if got := g.ProgressRatio(); got != 1.0 {
		t.Fatalf("ProgressRatio() = %v, want 1.0", got)
	}
}

func TestProgressRatio_Partial(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b", "c", "d"},
		CompletedCriteria: []string{"a"},
	}
	if got := g.ProgressRatio(); got != 0.25 {
		t.Fatalf("ProgressRatio() = %v, want 0.25", got)
	}
}

func TestIsStalled_BelowThreshold(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b"},
		TurnsActive:     5,
	}
	if g.IsStalled(10) {
		t.Fatal("should not be stalled when TurnsActive <= threshold")
	}
}

func TestIsStalled_AboveThresholdNoProgress(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
		TurnsActive:     15,
	}
	if !g.IsStalled(10) {
		t.Fatal("should be stalled when TurnsActive > threshold and progress < 10%")
	}
}

func TestIsStalled_AboveThresholdWithProgress(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
		TurnsActive:       15,
	}
	if g.IsStalled(10) {
		t.Fatal("should not be stalled when progress >= 10%")
	}
}

func TestMarkCompleted_Deduplicates(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
	}
	g.MarkCompleted([]string{"a", "b", "b"})
	if len(g.CompletedCriteria) != 2 {
		t.Fatalf("got %d completed, want 2", len(g.CompletedCriteria))
	}
}

func TestMarkCompleted_SkipsEmpty(t *testing.T) {
	g := AgentGoal{SuccessCriteria: []string{"a"}}
	g.MarkCompleted([]string{"", "  ", "a"})
	if len(g.CompletedCriteria) != 1 {
		t.Fatalf("got %d completed, want 1", len(g.CompletedCriteria))
	}
}

func TestMarkCompleted_RemovesFromBlocked(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b"},
		BlockedCriteria: []string{"a", "b"},
	}
	g.MarkCompleted([]string{"a"})
	if len(g.BlockedCriteria) != 1 || g.BlockedCriteria[0] != "b" {
		t.Fatalf("blocked = %v, want [b]", g.BlockedCriteria)
	}
}

func TestMarkBlocked_ExcludesCompleted(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
	}
	g.MarkBlocked([]string{"a", "b"})
	if len(g.BlockedCriteria) != 1 || g.BlockedCriteria[0] != "b" {
		t.Fatalf("blocked = %v, want [b]", g.BlockedCriteria)
	}
}

func TestMarkBlocked_Deduplicates(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a"},
		BlockedCriteria: []string{"a"},
	}
	g.MarkBlocked([]string{"a"})
	if len(g.BlockedCriteria) != 1 {
		t.Fatalf("got %d blocked, want 1", len(g.BlockedCriteria))
	}
}

func TestGoalTracker_AfterTurn_StallDetection(t *testing.T) {
	sink := &mockEventSink{}
	gt := NewGoalTracker(sink, "task-1", "project-1", WithStallThreshold(3))
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
	})

	for i := 0; i < 3; i++ {
		if stalled := gt.AfterTurn(context.Background(), nil, nil); stalled {
			t.Fatalf("stalled too early at turn %d", i+1)
		}
	}
	if stalled := gt.AfterTurn(context.Background(), nil, nil); !stalled {
		t.Fatal("expected stall after 4 turns with no progress (threshold=3)")
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 GOAL_STALLED event, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeGoalStalled {
		t.Fatalf("event type = %s, want GOAL_STALLED", sink.events[0].Type)
	}
}

func TestGoalTracker_AfterTurn_ProgressPreventsStall(t *testing.T) {
	sink := &mockEventSink{}
	gt := NewGoalTracker(sink, "task-1", "project-1", WithStallThreshold(3))
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
	})

	for i := 0; i < 3; i++ {
		gt.AfterTurn(context.Background(), nil, nil)
	}
	// complete 1 of 3 = 33%, well above 10%
	if stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil); stalled {
		t.Fatal("should not stall with 33% progress")
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(sink.events))
	}
}

func TestGoalTracker_AfterTurn_NilGoal(t *testing.T) {
	gt := NewGoalTracker(nil, "task-1", "project-1")
	if stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil); stalled {
		t.Fatal("should not stall with nil goal")
	}
}

func TestGoalTracker_Goal_ReturnsSnapshot(t *testing.T) {
	gt := NewGoalTracker(nil, "task-1", "project-1")
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a"},
	})
	snap := gt.Goal()
	snap.SuccessCriteria = append(snap.SuccessCriteria, "mutated")
	original := gt.Goal()
	if len(original.SuccessCriteria) != 1 {
		t.Fatal("mutation leaked through snapshot")
	}
}

func TestGoalFromTask_NoCriteria(t *testing.T) {
	task := models.Task{Description: "do stuff"}
	if g := GoalFromTask(task); g != nil {
		t.Fatal("expected nil goal when no success criteria")
	}
}

func TestGoalFromTask_WithCriteria(t *testing.T) {
	task := models.Task{
		Description:     "do stuff",
		SuccessCriteria: []string{"pass tests", "lint clean"},
	}
	g := GoalFromTask(task)
	if g == nil {
		t.Fatal("expected non-nil goal")
	}
	if len(g.SuccessCriteria) != 2 {
		t.Fatalf("got %d criteria, want 2", len(g.SuccessCriteria))
	}
}

func TestParseGoalProgress(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantCompleted []string
		wantBlocked   []string
	}{
		{
			name:          "completed and blocked",
			content:       "[COMPLETED] pass tests\n[BLOCKED] missing API key\nsome other text",
			wantCompleted: []string{"pass tests"},
			wantBlocked:   []string{"missing API key"},
		},
		{
			name:          "no markers",
			content:       "just a normal response",
			wantCompleted: nil,
			wantBlocked:   nil,
		},
		{
			name:          "empty values ignored",
			content:       "[COMPLETED] \n[BLOCKED]",
			wantCompleted: nil,
			wantBlocked:   nil,
		},
		{
			name:          "multiple completed",
			content:       "[COMPLETED] a\n[COMPLETED] b",
			wantCompleted: []string{"a", "b"},
			wantBlocked:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, b := parseGoalProgress(tt.content)
			if !slicesEqual(c, tt.wantCompleted) {
				t.Errorf("completed = %v, want %v", c, tt.wantCompleted)
			}
			if !slicesEqual(b, tt.wantBlocked) {
				t.Errorf("blocked = %v, want %v", b, tt.wantBlocked)
			}
		})
	}
}

func TestGoalAwarePartition_CompletedCompressedFirst(t *testing.T) {
	cm := &ContextManager{}
	gt := NewGoalTracker(nil, "task-1", "project-1")
	gt.SetGoal(AgentGoal{
		SuccessCriteria:   []string{"criterion-A", "criterion-B"},
		CompletedCriteria: []string{"criterion-A"},
		BlockedCriteria:   []string{"criterion-B"},
	})
	cm.SetGoalTracker(gt)

	compressed := []Turn{
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "working on criterion-B"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "unrelated turn"}}},
	}
	working := []Turn{
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "solved criterion-A already"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "still investigating"}}},
	}

	newCompressed, newWorking := cm.goalAwarePartition(compressed, working)

	// The turn mentioning blocked criterion-B should be retained in working
	foundBlockedInWorking := false
	for _, t := range newWorking {
		for _, m := range t.Messages {
			if m.Content == "working on criterion-B" {
				foundBlockedInWorking = true
			}
		}
	}
	if !foundBlockedInWorking {
		t.Fatal("blocked-criteria turn should be retained in working zone")
	}

	// The turn mentioning only completed criterion-A should move to compressed
	foundCompletedInCompressed := false
	for _, t := range newCompressed {
		for _, m := range t.Messages {
			if m.Content == "solved criterion-A already" {
				foundCompletedInCompressed = true
			}
		}
	}
	if !foundCompletedInCompressed {
		t.Fatal("completed-criteria-only turn should be promoted to compressed zone")
	}

	// Total turns should be preserved
	if len(newCompressed)+len(newWorking) != 4 {
		t.Fatalf("total turns = %d, want 4", len(newCompressed)+len(newWorking))
	}
}

func TestGoalAwarePartition_NoTracker(t *testing.T) {
	cm := &ContextManager{}
	compressed := []Turn{{Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}}}}
	working := []Turn{{Messages: []spec.PromptMessage{{Role: "assistant", Content: "hello"}}}}

	c, w := cm.goalAwarePartition(compressed, working)
	if len(c) != 1 || len(w) != 1 {
		t.Fatal("should return unchanged when no goal tracker")
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
