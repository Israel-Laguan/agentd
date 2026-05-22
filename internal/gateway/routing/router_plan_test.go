package routing

import (
	"strings"
	"testing"
)

func TestPhasePlanningPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		maxTasks int
		want     string
	}{
		{0, "Create an agentd DraftPlan. Output ONLY valid JSON."},
		{-1, "Create an agentd DraftPlan. Output ONLY valid JSON."},
		{3, "at most 3 tasks"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := phasePlanningPrompt(tt.maxTasks)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("phasePlanningPrompt(%d) = %q, want substring %q", tt.maxTasks, got, tt.want)
			}
			if tt.maxTasks > 0 && !strings.Contains(got, "Plan Phase 2") {
				t.Fatalf("phasePlanningPrompt(%d) = %q, want phase-2 hint", tt.maxTasks, got)
			}
		})
	}
}
