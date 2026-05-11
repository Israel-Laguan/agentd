package models

import (
	"encoding/json"
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

func TestDraftPlan_UnmarshalJSON_Legacy(t *testing.T) {
	data := `{
		"ProjectName": "Legacy Project",
		"Description": "Legacy Description",
		"Tasks": [
			{
				"Title": "Legacy Task 1",
				"TempID": "lt1",
				"Assignee": "SYSTEM"
			}
		]
	}`
	var plan DraftPlan
	if err := json.Unmarshal([]byte(data), &plan); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if plan.ProjectName != "Legacy Project" {
		t.Errorf("expected ProjectName %q, got %q", "Legacy Project", plan.ProjectName)
	}
	if plan.Description != "Legacy Description" {
		t.Errorf("expected Description %q, got %q", "Legacy Description", plan.Description)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Title != "Legacy Task 1" {
		t.Errorf("expected task title %q, got %q", "Legacy Task 1", plan.Tasks[0].Title)
	}
	if plan.Tasks[0].TempID != "lt1" {
		t.Errorf("expected task TempID %q, got %q", "lt1", plan.Tasks[0].TempID)
	}
	if plan.Tasks[0].Assignee != TaskAssigneeSystem {
		t.Errorf("expected assignee %q, got %q", TaskAssigneeSystem, plan.Tasks[0].Assignee)
	}
}

func TestDraftTask_UnmarshalJSON_Legacy(t *testing.T) {
	data := `{
		"Title": "Legacy Task",
		"Description": "Legacy Desc",
		"ReferenceID": "r1",
		"TempID": "t1",
		"Assignee": "HUMAN",
		"DependsOn": ["t0"]
	}`
	var task DraftTask
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if task.Title != "Legacy Task" {
		t.Errorf("expected Title %q, got %q", "Legacy Task", task.Title)
	}
	if task.Description != "Legacy Desc" {
		t.Errorf("expected Description %q, got %q", "Legacy Desc", task.Description)
	}
	if task.ReferenceID != "r1" {
		t.Errorf("expected ReferenceID %q, got %q", "r1", task.ReferenceID)
	}
	if task.TempID != "t1" {
		t.Errorf("expected TempID %q, got %q", "t1", task.TempID)
	}
	if task.Assignee != TaskAssigneeHuman {
		t.Errorf("expected Assignee %q, got %q", TaskAssigneeHuman, task.Assignee)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "t0" {
		t.Errorf("expected DependsOn [t0], got %v", task.DependsOn)
	}
}

func TestDraftTask_ID(t *testing.T) {
	tests := []struct {
		name string
		task DraftTask
		want string
	}{
		{
			name: "ReferenceID only",
			task: DraftTask{ReferenceID: "ref1"},
			want: "ref1",
		},
		{
			name: "TempID only",
			task: DraftTask{TempID: "temp1"},
			want: "temp1",
		},
		{
			name: "Both prefer ReferenceID",
			task: DraftTask{ReferenceID: "ref1", TempID: "temp1"},
			want: "ref1",
		},
		{
			name: "None",
			task: DraftTask{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.ID(); got != tt.want {
				t.Errorf("DraftTask.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}
