package models

import (
	"strings"
	"testing"
)

func TestDraftPlanValidateHappyPath(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "my project",
		Tasks: []DraftTask{
			{TempID: "a", Title: "Task A"},
			{TempID: "b", Title: "Task B", DependsOn: []string{"a"}},
		},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDraftPlanValidateEmptyProjectName(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "  ",
		Tasks:       []DraftTask{{TempID: "a", Title: "Task A"}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "project name is required") {
		t.Fatalf("Validate() error = %v, want project name required", err)
	}
}

func TestDraftPlanValidateNoTasks(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "project",
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one task") {
		t.Fatalf("Validate() error = %v, want at least one task", err)
	}
}

func TestDraftPlanValidateEmptyTitle(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "project",
		Tasks:       []DraftTask{{TempID: "a", Title: "  "}},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("Validate() error = %v, want title required", err)
	}
}

func TestDraftPlanValidateDuplicateTempID(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "project",
		Tasks: []DraftTask{
			{TempID: "x", Title: "A"},
			{TempID: "x", Title: "B"},
		},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate temp_id") {
		t.Fatalf("Validate() error = %v, want duplicate temp_id", err)
	}
}

func TestDraftPlanValidateDanglingDependency(t *testing.T) {
	plan := &DraftPlan{
		ProjectName: "project",
		Tasks: []DraftTask{
			{TempID: "a", Title: "A"},
			{TempID: "b", Title: "B", DependsOn: []string{"missing"}},
		},
	}
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), `depends_on "missing" not in plan`) {
		t.Fatalf("Validate() error = %v, want depends_on not in plan", err)
	}
}
