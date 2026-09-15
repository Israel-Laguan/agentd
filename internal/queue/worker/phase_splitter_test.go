package worker

import (
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
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
	g := agentruntime.NewBudgetGuard(nil, "t1")
	if !w.ShouldPlanWithBudget(long, g) {
		t.Fatal("ample budget should allow planning")
	}
	tight := &Worker{
		planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100},
		tokenBudget: 2500,
	}
	if tight.ShouldPlanWithBudget(long, g) {
		t.Fatal("budget below plan+execution reserve should skip planning")
	}
	tracker := gateway.NewBudgetTracker(5000)
	gUsed := agentruntime.NewBudgetGuard(tracker, "t1")
	tracker.Add("t1", 4000)
	wUsed := &Worker{
		planningCfg: config.AgenticPlanningConfig{ComplexityThreshold: 100},
		tokenBudget: 5000,
	}
	if wUsed.ShouldPlanWithBudget(long, gUsed) {
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
	if err := (&agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: "a", Action: "do"}}}).Validate(); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	dup := &agentcontext.Plan{Steps: []agentcontext.PlanStep{
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
		p := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: id, Action: "do"}}}
		if err := p.Validate(); err == nil {
			t.Fatalf("id %q: expected validation error", id)
		}
	}
}

func TestPlan_Validate_NormalizesStepID(t *testing.T) {
	t.Parallel()
	p := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: " analyze ", Action: " review "}}}
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
	plan := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: "analyze", Action: "review", OutputFormat: "text"}}}
	msgs := w.InjectPlan(nil, plan)
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
	plan := &agentcontext.Plan{Steps: []agentcontext.PlanStep{{ID: "analyze", Action: "review", OutputFormat: "text"}}}
	msgs := w.InjectPlan([]gateway.PromptMessage{
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

func TestShouldRunTiered_Disabled(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: false, ComplexityThreshold: 200}}
	task := models.Task{Title: "x", Description: strings.Repeat("a", 300)}
	if w.ShouldRunTiered(task) {
		t.Fatal("ShouldRunTiered should return false when disabled")
	}
}

func TestShouldRunTiered_Enabled_BelowThreshold(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 200}}
	short := models.Task{Title: "x", Description: "y"}
	if w.ShouldRunTiered(short) {
		t.Fatal("ShouldRunTiered should return false for short task below threshold")
	}
}

func TestShouldRunTiered_Enabled_AtThreshold(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 100}}
	// title 2 + desc 98 = 100 (at threshold)
	task := models.Task{Title: "ab", Description: strings.Repeat("a", 98)}
	if !w.ShouldRunTiered(task) {
		t.Fatal("ShouldRunTiered should return true at threshold")
	}
}

func TestShouldRunTiered_Enabled_AboveThreshold(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 100}}
	long := models.Task{Title: "x", Description: strings.Repeat("a", 200)}
	if !w.ShouldRunTiered(long) {
		t.Fatal("ShouldRunTiered should return true above threshold")
	}
}

func TestShouldRunTiered_Enabled_BelowThreshold_OneShot(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 200}}
	simple := models.Task{Title: "x", Description: "y"}
	if w.ShouldRunTiered(simple) {
		t.Fatal("simple task should stay one-shot even when tiered is enabled")
	}
}

func TestShouldRunTiered_ThresholdZero_DisablesSplitting(t *testing.T) {
	t.Parallel()
	w := &Worker{tieredCfg: config.TieredConfig{Enabled: true, ComplexityThreshold: 0}}
	long := models.Task{Title: "x", Description: strings.Repeat("a", 500)}
	if w.ShouldRunTiered(long) {
		t.Fatal("threshold 0 should disable splitting even when enabled")
	}
}
