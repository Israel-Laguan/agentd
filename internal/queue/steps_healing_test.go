package queue

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cucumber/godog"

	"agentd/internal/config"
	"agentd/internal/models"
)

// --- Healing / phase-planning steps ---

func registerHealingSteps(sc *godog.ScenarioContext, state *healingScenario) {
	// Self-healing
	sc.Step(`^a task that has failed (\d+) times? with self-healing enabled$`, state.taskFailedNTimes)
	sc.Step(`^the worker applies the healing ladder for retry (\d+)$`, state.applyHealingLadder)
	sc.Step(`^the healing action should be "([^"]*)" with step "([^"]*)"$`, state.healingActionShouldBe)
	sc.Step(`^a TUNE event should be emitted$`, state.tuneEventEmitted)

	// Phase planning
	sc.Step(`^a task titled "([^"]*)"$`, state.taskTitled)
	sc.Step(`^it should be recognized as a phase-planning task$`, state.isPhasePlanningTask)
	sc.Step(`^it should not be recognized as a phase-planning task$`, state.isNotPhasePlanningTask)
	sc.Step(`^a planning task titled "([^"]*)"$`, state.taskTitled)
	sc.Step(`^continuation tasks are retitled$`, state.retitleContinuation)
	sc.Step(`^the next continuation should be titled "([^"]*)"$`, state.continuationTitled)
}

type healingScenario struct {
	tuner         *ParameterTuner
	profile       models.AgentProfile
	lastAction    HealingAction
	taskTitle     string
	retitledTasks []models.DraftTask
}

func newHealingScenario() *healingScenario {
	return &healingScenario{
		tuner: NewParameterTuner(config.HealingConfig{
			Enabled:           true,
			Strategy:          config.HealingStrategyIncreaseEffort,
			ContextMultiplier: 2,
		}),
		profile: models.AgentProfile{
			ID: "default", Temperature: 0.5, Model: "gpt-4",
			SystemPrompt: sql.NullString{String: "Return JSON.", Valid: true},
		},
	}
}

func (s *healingScenario) taskFailedNTimes(_ context.Context, _ int) error {
	return nil
}

func (s *healingScenario) applyHealingLadder(_ context.Context, retryCount int) error {
	s.lastAction = s.tuner.ForAttempt(retryCount, s.profile)
	return nil
}

func (s *healingScenario) healingActionShouldBe(_ context.Context, actionType, stepName string) error {
	if string(s.lastAction.Type) != actionType {
		return fmt.Errorf("action type = %q, want %q", s.lastAction.Type, actionType)
	}
	if s.lastAction.StepName != stepName {
		return fmt.Errorf("step name = %q, want %q", s.lastAction.StepName, stepName)
	}
	return nil
}

func (s *healingScenario) tuneEventEmitted(context.Context) error {
	if s.lastAction.Type != HealingActionTune {
		return fmt.Errorf("last action type = %q, not tune", s.lastAction.Type)
	}
	return nil
}

func (s *healingScenario) taskTitled(_ context.Context, title string) error {
	s.taskTitle = title
	return nil
}

func (s *healingScenario) isPhasePlanningTask(context.Context) error {
	if !IsPhasePlanningTask(s.taskTitle) {
		return fmt.Errorf("%q should be a phase-planning task", s.taskTitle)
	}
	return nil
}

func (s *healingScenario) isNotPhasePlanningTask(context.Context) error {
	if IsPhasePlanningTask(s.taskTitle) {
		return fmt.Errorf("%q should not be a phase-planning task", s.taskTitle)
	}
	return nil
}

func (s *healingScenario) retitleContinuation(context.Context) error {
	input := []models.DraftTask{
		{Title: "Build component A"},
		{Title: "Plan Phase 2"},
	}
	s.retitledTasks = RetitlePhaseContinuationTasks(input, NextPhaseNumber(s.taskTitle))
	return nil
}

func (s *healingScenario) continuationTitled(_ context.Context, want string) error {
	for _, t := range s.retitledTasks {
		if t.Title == want {
			return nil
		}
	}
	titles := make([]string, len(s.retitledTasks))
	for i, t := range s.retitledTasks {
		titles[i] = t.Title
	}
	return fmt.Errorf("no task with title %q, got %v", want, titles)
}
