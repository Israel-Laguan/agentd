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
