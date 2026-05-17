package domain

import (
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestValidateTaskCap(t *testing.T) {
	plan := models.DraftPlan{Tasks: []models.DraftTask{{Title: "one"}, {Title: "two"}}}

	if err := ValidateTaskCap(plan, 2); err != nil {
		t.Fatalf("ValidateTaskCap() error = %v", err)
	}
	if err := ValidateTaskCap(plan, 0); err != nil {
		t.Fatalf("ValidateTaskCap(disabled) error = %v", err)
	}
	err := ValidateTaskCap(plan, 1)
	if !errors.Is(err, models.ErrInvalidDraftPlan) {
		t.Fatalf("ValidateTaskCap() error = %v, want ErrInvalidDraftPlan", err)
	}
}

func TestNormalizeDraftPlan(t *testing.T) {
	tests := []struct {
		name    string
		plan    models.DraftPlan
		wantErr bool
		errIs   error
	}{
		{
			name: "valid plan",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks: []models.DraftTask{
					{Title: "Task 1", ReferenceID: "t1"},
					{Title: "Task 2", ReferenceID: "t2", DependsOn: []string{"t1"}},
				},
			},
			wantErr: false,
		},
		{
			name: "empty project name",
			plan: models.DraftPlan{
				ProjectName: "  ",
				Tasks: []models.DraftTask{
					{Title: "Task 1"},
				},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
		{
			name: "no tasks",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks:       []models.DraftTask{},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
		{
			name: "task missing title",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks: []models.DraftTask{
					{Title: ""},
				},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
		{
			name: "duplicate task id",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks: []models.DraftTask{
					{Title: "Task 1", ReferenceID: "t1"},
					{Title: "Task 2", ReferenceID: "t1"},
				},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
		{
			name: "unknown dependency",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks: []models.DraftTask{
					{Title: "Task 1", ReferenceID: "t1", DependsOn: []string{"unknown"}},
				},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
		{
			name: "invalid assignee",
			plan: models.DraftPlan{
				ProjectName: "Test Project",
				Tasks: []models.DraftTask{
					{Title: "Task 1", Assignee: "invalid"},
				},
			},
			wantErr: true,
			errIs:   models.ErrInvalidDraftPlan,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeDraftPlan(tt.plan)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeDraftPlan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("NormalizeDraftPlan() error = %v, wantErrIs %v", err, tt.errIs)
			}
		})
	}
}

func TestNormalizeDraftPlan_Defaults(t *testing.T) {
	plan := models.DraftPlan{
		ProjectName: "Test",
		Tasks: []models.DraftTask{
			{Title: "Task 1"},
		},
	}
	normalized, err := NormalizeDraftPlan(plan)
	if err != nil {
		t.Fatalf("NormalizeDraftPlan() error = %v", err)
	}

	if normalized.Tasks[0].ReferenceID != "task-1" {
		t.Errorf("expected default ReferenceID task-1, got %q", normalized.Tasks[0].ReferenceID)
	}
	if normalized.Tasks[0].TempID != "task-1" {
		t.Errorf("expected default TempID task-1, got %q", normalized.Tasks[0].TempID)
	}
	if normalized.Tasks[0].Assignee != models.TaskAssigneeSystem {
		t.Errorf("expected default assignee %q, got %q", models.TaskAssigneeSystem, normalized.Tasks[0].Assignee)
	}
}

func TestValidateDAG(t *testing.T) {
	tests := []struct {
		name    string
		plan    models.DraftPlan
		wantErr bool
	}{
		{
			name: "linear",
			plan: models.DraftPlan{
				Tasks: []models.DraftTask{
					{ReferenceID: "t1"},
					{ReferenceID: "t2", DependsOn: []string{"t1"}},
				},
			},
			wantErr: false,
		},
		{
			name: "circular self",
			plan: models.DraftPlan{
				Tasks: []models.DraftTask{
					{ReferenceID: "t1", DependsOn: []string{"t1"}},
				},
			},
			wantErr: true,
		},
		{
			name: "circular simple",
			plan: models.DraftPlan{
				Tasks: []models.DraftTask{
					{ReferenceID: "t1", DependsOn: []string{"t2"}},
					{ReferenceID: "t2", DependsOn: []string{"t1"}},
				},
			},
			wantErr: true,
		},
		{
			name: "circular complex",
			plan: models.DraftPlan{
				Tasks: []models.DraftTask{
					{ReferenceID: "t1", DependsOn: []string{"t3"}},
					{ReferenceID: "t2", DependsOn: []string{"t1"}},
					{ReferenceID: "t3", DependsOn: []string{"t2"}},
				},
			},
			wantErr: true,
		},
		{
			name: "diamond",
			plan: models.DraftPlan{
				Tasks: []models.DraftTask{
					{ReferenceID: "t1"},
					{ReferenceID: "t2", DependsOn: []string{"t1"}},
					{ReferenceID: "t3", DependsOn: []string{"t1"}},
					{ReferenceID: "t4", DependsOn: []string{"t2", "t3"}},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDAG(tt.plan)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDAG() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, models.ErrCircularDependency) {
				t.Errorf("ValidateDAG() error = %v, want ErrCircularDependency", err)
			}
		})
	}
}
