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

func TestMessageMentionsCriterion(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		criterion string
		want      bool
	}{
		{name: "marker completed", content: "[COMPLETED] pass tests", criterion: "pass tests", want: true},
		{name: "marker blocked case insensitive", content: "[blocked] API key", criterion: "API key", want: true},
		{name: "word boundary", content: "need to pass tests today", criterion: "pass tests", want: true},
		{name: "substring false positive", content: "running testing suite", criterion: "test", want: false},
		{name: "case insensitive word", content: "PASS TESTS done", criterion: "pass tests", want: true},
		{name: "no match", content: "unrelated content", criterion: "pass tests", want: false},
		{
			name:      "unicode prefix before marker line",
			content:   "İ\n[COMPLETED] pass tests",
			criterion: "pass tests",
			want:      true,
		},
		{
			name:      "mid-line marker ignored",
			content:   "prefix [COMPLETED] passtests",
			criterion: "pass tests",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageMentionsCriterion(tt.content, tt.criterion); got != tt.want {
				t.Fatalf("messageMentionsCriterion(%q, %q) = %v, want %v", tt.content, tt.criterion, got, tt.want)
			}
		})
	}
}

func TestGoalAwarePartition_BlockedExpandsWorkingSuffix(t *testing.T) {
	cm := &ContextManager{}
	gt := NewGoalTracker("task-1", "project-1")
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"criterion-A", "criterion-B"},
		BlockedCriteria: []string{"criterion-B"},
	})
	cm.SetGoalTracker(gt)

	turns := []Turn{
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "turn-1"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "working on criterion-B"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "solved criterion-A already"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "still investigating"}}},
	}

	newCompressed, newWorking := cm.goalAwarePartition(turns, 2)

	if !turnsContainContent(newWorking, "working on criterion-B") {
		t.Fatal("blocked-criteria turn should be retained in working zone")
	}
	if turnsContainContent(newCompressed, "working on criterion-B") {
		t.Fatal("blocked-criteria turn should not remain in compressed zone")
	}
	if !turnsContainContent(newWorking, "solved criterion-A already") {
		t.Fatal("completed-only turn in working should stay in working zone")
	}
	if len(newCompressed)+len(newWorking) != 4 {
		t.Fatalf("total turns = %d, want 4", len(newCompressed)+len(newWorking))
	}
}

func TestGoalAwarePartition_PreservesChronology(t *testing.T) {
	cm := &ContextManager{}
	gt := NewGoalTracker("task-1", "project-1")
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"criterion-B"},
		BlockedCriteria: []string{"criterion-B"},
	})
	cm.SetGoalTracker(gt)

	turns := []Turn{
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "turn-1"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "turn-2 criterion-B"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "turn-3"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "turn-4"}}},
	}

	compressed, working := cm.goalAwarePartition(turns, 2)
	merged := append(compressed, working...)
	for i, want := range []string{"turn-1", "turn-2 criterion-B", "turn-3", "turn-4"} {
		if !turnsContainContent([]Turn{merged[i]}, want) {
			t.Fatalf("turn %d content = %q, want %q", i, merged[i].Messages[0].Content, want)
		}
	}
}

func TestGoalAwarePartition_NoTracker(t *testing.T) {
	cm := &ContextManager{}
	turns := []Turn{
		{Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}}},
		{Messages: []spec.PromptMessage{{Role: "assistant", Content: "hello"}}},
	}

	c, w := cm.goalAwarePartition(turns, 1)
	if len(c) != 1 || len(w) != 1 {
		t.Fatal("should return unchanged split when no goal tracker")
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
