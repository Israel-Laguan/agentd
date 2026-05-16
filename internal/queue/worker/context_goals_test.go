package worker

import (
	"reflect"
	"testing"

	"agentd/internal/gateway/spec"
)

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
			name:    "no markers",
			content: "just a normal response",
		},
		{
			name:    "empty values ignored",
			content: "[COMPLETED] \n[BLOCKED]",
		},
		{
			name:          "multiple completed",
			content:       "[COMPLETED] a\n[COMPLETED] b",
			wantCompleted: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, b := parseGoalProgress(tt.content)
			if !reflect.DeepEqual(c, tt.wantCompleted) {
				t.Errorf("completed = %v, want %v", c, tt.wantCompleted)
			}
			if !reflect.DeepEqual(b, tt.wantBlocked) {
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

	if !turnsContainContent(newWorking, "working on criterion-B") {
		t.Fatal("blocked-criteria turn should be retained in working zone")
	}
	if !turnsContainContent(newCompressed, "solved criterion-A already") {
		t.Fatal("completed-criteria-only turn should be promoted to compressed zone")
	}
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

func turnsContainContent(turns []Turn, content string) bool {
	for _, turn := range turns {
		for _, msg := range turn.Messages {
			if msg.Content == content {
				return true
			}
		}
	}
	return false
}
