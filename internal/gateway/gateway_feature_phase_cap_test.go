package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/providers"
	"agentd/internal/models"
)

func (s *gatewayScenario) setPhaseCap(_ context.Context, cap int) error {
	s.phaseCap = cap
	return nil
}

func (s *gatewayScenario) llmReturnsDraftPlan(_ context.Context, taskCount int) error {
	plan := models.DraftPlan{ProjectName: "Project", Description: "Generated project"}
	for i := 1; i <= taskCount; i++ {
		plan.Tasks = append(plan.Tasks, models.DraftTask{
			TempID:      fmt.Sprintf("task-%d", i),
			Title:       fmt.Sprintf("Task %d", i),
			Description: fmt.Sprintf("Do task %d", i),
		})
	}
	s.llmTaskCount = taskCount
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	s.providers = append(s.providers, &fakeProvider{
		providerName: "mock",
		resp:         AIResponse{Content: string(data), ProviderUsed: "mock"},
	})
	return nil
}

func (s *gatewayScenario) generatePlan(_ context.Context, intent string) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	router := NewRouter(provs...).WithPhaseCap(s.phaseCap)
	plan, err := router.GeneratePlan(context.Background(), intent)
	if err != nil {
		s.aiErr = err
		return nil
	}
	s.plan = plan
	return nil
}

func (s *gatewayScenario) planShouldHaveTaskCount(_ context.Context, want int) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil (err = %v)", s.aiErr)
	}
	if len(s.plan.Tasks) != want {
		return fmt.Errorf("task count = %d, want %d", len(s.plan.Tasks), want)
	}
	return nil
}

func (s *gatewayScenario) firstNTasksMatchOriginalOrder(_ context.Context, n int) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil")
	}
	for i := 0; i < n && i < len(s.plan.Tasks); i++ {
		want := fmt.Sprintf("Task %d", i+1)
		if s.plan.Tasks[i].Title != want {
			return fmt.Errorf("task[%d].Title = %q, want %q", i, s.plan.Tasks[i].Title, want)
		}
	}
	return nil
}

func (s *gatewayScenario) nthTaskTitled(_ context.Context, n int, title string) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil")
	}
	idx := n - 1
	if idx >= len(s.plan.Tasks) {
		return fmt.Errorf("plan has %d tasks, cannot check task %d", len(s.plan.Tasks), n)
	}
	if s.plan.Tasks[idx].Title != title {
		return fmt.Errorf("task[%d].Title = %q, want %q", idx, s.plan.Tasks[idx].Title, title)
	}
	return nil
}

func (s *gatewayScenario) continuationReferencesRemaining(context.Context) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil")
	}
	last := s.plan.Tasks[len(s.plan.Tasks)-1]
	if last.Title != "Plan Phase 2" {
		return fmt.Errorf("last task %q is not a continuation", last.Title)
	}
	cap := s.phaseCap
	for i := cap; i <= s.llmTaskCount; i++ {
		ref := fmt.Sprintf("Task %d", i)
		if !strings.Contains(last.Description, ref) {
			return fmt.Errorf("continuation description missing %q: %s", ref, last.Description)
		}
	}
	return nil
}

func (s *gatewayScenario) noTaskTitled(_ context.Context, title string) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil")
	}
	for _, t := range s.plan.Tasks {
		if t.Title == title {
			return fmt.Errorf("unexpected task titled %q found", title)
		}
	}
	return nil
}
