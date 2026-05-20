package worker

import (
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestEstimateTaskComplexity(t *testing.T) {
	t.Parallel()
	task := models.Task{
		Title:       "ab",
		Description: "line1\nline2\nxxx",
	}
	got := EstimateTaskComplexity(task)
	// title 2 + desc 15 + 2 newlines * 10 = 37
	if got != 37 {
		t.Fatalf("EstimateTaskComplexity() = %d, want 37", got)
	}
}

func TestShouldPlanWithBudget(t *testing.T) {
	t.Parallel()
	long := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, Title: "x", Description: strings.Repeat("a", 200)}
	w := &Worker{
		planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100},
		tokenBudget: 10000,
	}
	g := NewBudgetGuard(nil, "t1")
	if !w.shouldPlanWithBudget(long, g) {
		t.Fatal("ample budget should allow planning")
	}
	tight := &Worker{
		planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100},
		tokenBudget: 2500,
	}
	if tight.shouldPlanWithBudget(long, g) {
		t.Fatal("budget below plan+execution reserve should skip planning")
	}
	tracker := gateway.NewBudgetTracker(5000)
	gUsed := NewBudgetGuard(tracker, "t1")
	tracker.Add("t1", 4000)
	wUsed := &Worker{
		planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100},
		tokenBudget: 5000,
	}
	if wUsed.shouldPlanWithBudget(long, gUsed) {
		t.Fatal("high prior usage should skip planning")
	}
}

func TestShouldPlan(t *testing.T) {
	t.Parallel()
	w := &Worker{planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100}}
	short := models.Task{Title: "x", Description: "y"}
	if w.shouldPlan(short) {
		t.Fatal("short task should not plan")
	}
	long := models.Task{Title: "x", Description: strings.Repeat("a", 200)}
	if !w.shouldPlan(long) {
		t.Fatal("long task should plan")
	}
	disabled := &Worker{planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 0}}
	if disabled.shouldPlan(long) {
		t.Fatal("threshold 0 should disable planning")
	}
}

func TestPlan_Validate(t *testing.T) {
	t.Parallel()
	if err := (&Plan{Steps: []PlanStep{{ID: "a", Action: "do"}}}).Validate(); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	dup := &Plan{Steps: []PlanStep{
		{ID: "a", Action: "one"},
		{ID: "a", Action: "two"},
	}}
	if err := dup.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate id: err = %v", err)
	}
}

func TestPlan_Validate_RejectsInvalidStepID(t *testing.T) {
	t.Parallel()
	cases := []string{"bad id", "<!--", "UPPER", ""}
	for _, id := range cases {
		p := &Plan{Steps: []PlanStep{{ID: id, Action: "do"}}}
		if err := p.Validate(); err == nil {
			t.Fatalf("id %q: expected validation error", id)
		}
	}
}

func TestPlan_Validate_NormalizesStepID(t *testing.T) {
	t.Parallel()
	p := &Plan{Steps: []PlanStep{{ID: " analyze ", Action: " review "}}}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid plan with padded id: %v", err)
	}
	if p.Steps[0].ID != "analyze" {
		t.Fatalf("id = %q, want analyze", p.Steps[0].ID)
	}
	if p.Steps[0].Action != "review" {
		t.Fatalf("action = %q, want review", p.Steps[0].Action)
	}
}

func TestInjectPlan_EmptyMessages(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	plan := &Plan{Steps: []PlanStep{{ID: "analyze", Action: "review", OutputFormat: "text"}}}
	msgs := w.injectPlan(nil, plan)
	if len(msgs) != 1 || msgs[0].Role != "system" {
		t.Fatalf("expected single system message, got %v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "WORK PLAN") {
		t.Fatalf("system prompt missing plan block: %q", msgs[0].Content)
	}
}

func TestInjectPlan(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	plan := &Plan{Steps: []PlanStep{{ID: "analyze", Action: "review", OutputFormat: "text"}}}
	msgs := w.injectPlan([]gateway.PromptMessage{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "task"},
	}, plan)
	if !strings.Contains(msgs[0].Content, "WORK PLAN") {
		t.Fatalf("system prompt missing plan block: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, `"analyze"`) {
		t.Fatalf("system prompt missing step id: %q", msgs[0].Content)
	}
}

func TestBuildPlanContext_Truncates(t *testing.T) {
	t.Parallel()
	w := &Worker{planningCfg: config.AgenticPlanningConfig{PlanContextMaxChars: 10}}
	task := models.Task{Title: "t", Description: strings.Repeat("x", 100)}
	ctx := w.buildPlanContext(task, models.Project{BaseEntity: models.BaseEntity{ID: "p1"}})
	if !strings.Contains(ctx, "...[truncated]") {
		t.Fatalf("expected truncation marker in %q", ctx)
	}
}
