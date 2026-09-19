package worker

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestClassifyVerifyOutcome_Pass(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "pass", Detail: ""},
		},
		Overall: "pass",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomePass {
		t.Errorf("expected pass, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Fail(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "fail", Detail: "test failed"},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeFail {
		t.Errorf("expected fail, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Flake(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "go test ./...", Outcome: "flake", Detail: "intermittent"},
			{Check: "go vet ./...", Outcome: "pass", Detail: ""},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeFlake {
		t.Errorf("expected flake, got %v", outcome)
	}
}

func TestClassifyVerifyOutcome_Conflict(t *testing.T) {
	result := VerifyResult{
		Results: []CheckResult{
			{Check: "merge conflicts", Outcome: "conflict", Detail: "merge conflict detected"},
		},
		Overall: "fail",
	}
	outcome := ClassifyVerifyOutcome(result)
	if outcome != models.VerifyOutcomeConflict {
		t.Errorf("expected conflict, got %v", outcome)
	}
}

func TestHandleVerifyOutcome_Pass(t *testing.T) {
	ctx := context.Background()
	w := &Worker{}

	parentTask := models.Task{
		BaseEntity: models.BaseEntity{ID: "parent-1"},
		Title:      "complex task",
		State:      models.TaskStateRunning,
	}

	verifyTask := models.Task{
		BaseEntity: models.BaseEntity{ID: "verify-1"},
		Title:      "verify step",
		State:      models.TaskStateRunning,
	}

	// This test would need a mock datastore
	// For now, just verify the logic compiles and the function signature is correct
	_ = w
	_ = ctx
	_ = parentTask
	_ = verifyTask
}

func TestVerifyResultOutcome_Valid(t *testing.T) {
	tests := []struct {
		outcome models.VerifyResultOutcome
		want    bool
	}{
		{models.VerifyOutcomePass, true},
		{models.VerifyOutcomeFail, true},
		{models.VerifyOutcomeFlake, true},
		{models.VerifyOutcomeConflict, true},
		{models.VerifyResultOutcome("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			if got := tt.outcome.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}
