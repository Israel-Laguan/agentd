package intake

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
)

func TestIntakeFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeIntakeScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run intake feature tests")
	}
}

type intakeScenario struct {
	phaseCap     int
	llmReply     string
	llmTaskCount int
	plan         *models.DraftPlan
	planErr      error
}

func initializeIntakeScenario(sc *godog.ScenarioContext) {
	state := &intakeScenario{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*state = intakeScenario{}
		return ctx, nil
	})
	sc.Step(`^the gateway max_tasks_per_phase is (\d+)$`, state.setPhaseCap)
	sc.Step(`^the LLM returns a DraftPlan with (\d+) tasks$`, state.llmReturnsDraftPlan)
	sc.Step(`^the intake plan processes the intent "([^"]*)"$`, state.planProcessesIntent)
	sc.Step(`^the returned DraftPlan should contain exactly (\d+) tasks$`, state.planShouldHaveTaskCount)
	sc.Step(`^the first (\d+) tasks should match the original order$`, state.firstNTasksMatchOriginalOrder)
	sc.Step(`^the (\d+)(?:st|nd|rd|th) task should be titled "([^"]*)"$`, state.nthTaskTitled)
	sc.Step(`^the continuation task description should reference the remaining work$`, state.continuationReferencesRemaining)
	sc.Step(`^no task should be titled "([^"]*)"$`, state.noTaskTitled)
}

func (s *intakeScenario) setPhaseCap(_ context.Context, n int) error {
	s.phaseCap = n
	return nil
}

func (s *intakeScenario) llmReturnsDraftPlan(_ context.Context, taskCount int) error {
	raw, err := draftPlanJSON(taskCount)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	s.llmReply = raw
	s.llmTaskCount = taskCount
	return nil
}

func (s *intakeScenario) planProcessesIntent(ctx context.Context, intent string) error {
	gw := &fakeGateway{replies: []string{s.llmReply}}
	plan, err := Plan(ctx, stage.NewModelAgent(gw), spec.PurposeIntakeDraftPlan, s.phaseCap, intent)
	if err != nil {
		s.planErr = err
		return nil
	}
	s.plan = plan
	return nil
}

func (s *intakeScenario) planShouldHaveTaskCount(_ context.Context, want int) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil (err = %v)", s.planErr)
	}
	if len(s.plan.Tasks) != want {
		return fmt.Errorf("task count = %d, want %d", len(s.plan.Tasks), want)
	}
	return nil
}

func (s *intakeScenario) firstNTasksMatchOriginalOrder(_ context.Context, n int) error {
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

func (s *intakeScenario) nthTaskTitled(_ context.Context, n int, title string) error {
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

func (s *intakeScenario) continuationReferencesRemaining(context.Context) error {
	if s.plan == nil {
		return fmt.Errorf("plan is nil")
	}
	last := s.plan.Tasks[len(s.plan.Tasks)-1]
	if last.Title != "Plan Phase 2" {
		return fmt.Errorf("last task %q is not a continuation", last.Title)
	}
	for i := s.phaseCap; i <= s.llmTaskCount; i++ {
		ref := fmt.Sprintf("Task %d", i)
		if !strings.Contains(last.Description, ref) {
			return fmt.Errorf("continuation description missing %q: %s", ref, last.Description)
		}
	}
	return nil
}

func (s *intakeScenario) noTaskTitled(_ context.Context, title string) error {
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
