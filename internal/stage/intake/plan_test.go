package intake

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
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

// B-053: the planner must name the final host-fact task with the title the
// host report renderer recognizes, or the renderer never fires and the report
// is raw command output again (B-045).
func TestPlanningPromptNamesTheSystemReportTask(t *testing.T) {
	t.Parallel()
	for _, maxTasks := range []int{0, 5} {
		prompt := phasePlanningPrompt(maxTasks)
		if !strings.Contains(prompt, `"`+models.HostReportTaskTitle+`"`) {
			t.Fatalf("planning prompt (max %d) does not ask for a task titled %q:\n%s", maxTasks, models.HostReportTaskTitle, prompt)
		}
	}
}

func TestPhasePlanningPrompt_StatesDraftPlanSchema(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, 3} {
		got := phasePlanningPrompt(max)
		for _, want := range []string{"project_name", "tasks", "title", "description", "assignee"} {
			if !strings.Contains(got, want) {
				t.Fatalf("phasePlanningPrompt(%d) missing %q:\n%s", max, want, got)
			}
		}
	}
}

func TestPhasePlanningPrompt_GuidesHostFactPlans(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, 3} {
		got := phasePlanningPrompt(max)
		for _, want := range []string{"hostnamectl", "lscpu", "report.md", "/etc, /proc or /sys", "depends_on", "bare relative file names"} {
			if !strings.Contains(got, want) {
				t.Fatalf("phasePlanningPrompt(%d) missing %q:\n%s", max, want, got)
			}
		}
	}
}

// B-085 / PR 11: the planner marks each task that must change the workspace,
// so the worker can hold a text-only finish of that task to evidence.
func TestPlanningPromptAsksForRequiresWorkspaceChange(t *testing.T) {
	t.Parallel()
	got := phasePlanningPrompt(0)
	for _, want := range []string{
		"Set requires_workspace_change to true on every task that must create or change a file, or run a command, in the project workspace.",
		"Set it to false, or leave it out, on a task that only reasons or writes text",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("planning prompt missing %q:\n%s", want, got)
		}
	}
}

func TestPlanCarriesRequiresWorkspaceChange(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{
		`{"project_name":"p","tasks":[{"title":"Write the file","description":"create main.go","assignee":"SYSTEM","requires_workspace_change":true},{"title":"Summarize","description":"s","assignee":"SYSTEM"}]}`,
	}}
	plan, err := Plan(context.Background(), stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, 0, "add a file")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.Tasks[0].RequiresWorkspaceChange || plan.Tasks[1].RequiresWorkspaceChange {
		t.Fatalf("tasks = %+v, want only the first marked", plan.Tasks)
	}
}
