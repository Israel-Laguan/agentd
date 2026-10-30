package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

// A stage that returns NeedsHuman hands its question to a person: the elicitor
// asks through the human handoff and reports ErrNeedsHuman so its caller stops.
func TestElicitorNeedsHumanAsksAPerson(t *testing.T) {
	t.Parallel()
	var asked []string
	e := NewElicitorOn(&stageRecorder{outcome: stage.OutcomeNeedsHuman}).WithHuman(
		func(_ context.Context, task models.Task, question string, _ []string) error {
			if task.ID != "task-elicit-human" {
				t.Errorf("asked for task %q, want task-elicit-human", task.ID)
			}
			asked = append(asked, question)
			return nil
		})

	_, err := e.Analyze(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-elicit-human"}}, models.Project{}, "")
	if !errors.Is(err, stage.ErrNeedsHuman) {
		t.Fatalf("Analyze() error = %v, want ErrNeedsHuman", err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "create.elicit") {
		t.Fatalf("human questions = %q, want one question naming create.elicit", asked)
	}
}

// Without a handoff the outcome still stops the caller: it must not continue
// as if the stage had answered.
func TestElicitorNeedsHumanWithoutHandoffStillStops(t *testing.T) {
	t.Parallel()
	e := NewElicitorOn(&stageRecorder{outcome: stage.OutcomeNeedsHuman})
	_, err := e.Analyze(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "task-no-handoff"}}, models.Project{}, "")
	if !errors.Is(err, stage.ErrNeedsHuman) {
		t.Fatalf("Analyze() error = %v, want ErrNeedsHuman", err)
	}
}

// needsHumanFixture is a task in the fake store, which supports the atomic
// clarification handoff that AskHuman needs.
func needsHumanFixture(t *testing.T) (*testutil.FakeKanbanStore, models.Task, models.Project, models.AgentProfile) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(t.TempDir())
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "human", Description: "d",
		Tasks: []models.DraftTask{{Title: "Ambiguous", Description: "do the thing"}}})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	task := tasks[0]
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	profile, err := store.GetAgentProfile(ctx, task.AgentID)
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	return store, task, *project, *profile
}

func clarificationChildren(t *testing.T, store *testutil.FakeKanbanStore, taskID string) (clarifications, others int) {
	t.Helper()
	children, err := store.ListChildTasks(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	for _, child := range children {
		if strings.HasPrefix(child.Title, models.HITLSubtaskTitleClarification) {
			clarifications++
		} else {
			others++
		}
	}
	return clarifications, others
}

// End to end: a split stage that returns NeedsHuman creates a HITL clarification
// subtask on the task and does not fall through to a healing handoff.
func TestHealingSplitNeedsHumanCreatesClarificationSubtask(t *testing.T) {
	t.Parallel()
	store, task, project, profile := needsHumanFixture(t)
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	w.stageAgent = &stageRecorder{outcome: stage.OutcomeNeedsHuman}

	w.handleHealingSplit(context.Background(), task, project, profile)

	if clarifications, others := clarificationChildren(t, store, task.ID); clarifications != 1 || others != 0 {
		t.Fatalf("children: clarification=%d other=%d, want one clarification and nothing else", clarifications, others)
	}
}

// The work plan's stage can hand the task to a person. GeneratePlan then reports
// ErrNeedsHuman so the agentic run does not start without its plan.
func TestWorkPlanNeedsHumanStopsTheRun(t *testing.T) {
	t.Parallel()
	store, task, project, _ := needsHumanFixture(t)
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())
	agent := &stageRecorder{outcome: stage.OutcomeNeedsHuman}
	w.stageAgent = agent

	_, err := w.GeneratePlan(context.Background(), task, project, nil)
	if !errors.Is(err, stage.ErrNeedsHuman) {
		t.Fatalf("GeneratePlan() error = %v, want ErrNeedsHuman", err)
	}
	if len(agent.reqs) != 1 || agent.reqs[0].Purpose != gateway.PurposeExecWorkPlan {
		t.Fatalf("stage calls = %+v, want one exec.work_plan call", agent.reqs)
	}
	if clarifications, _ := clarificationChildren(t, store, task.ID); clarifications != 1 {
		t.Fatalf("clarification subtasks = %d, want 1", clarifications)
	}
}

// A marked reply that names its question and options hands those to the person;
// one without a question falls back to the generic question for the purpose.
func TestNeedsHumanHandsOffTheStagesOwnQuestion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, output, wantQuestion string
		wantOptions                []string
	}{
		{"own question", `{"needs_human":true,"question":"Which repo?","options":["api","web"]}`, "Which repo?", []string{"api", "web"}},
		{"padded and blank options", `{"needs_human":true,"question":"  Which repo? ","options":[" api ","  ",""]}`, "Which repo?", []string{"api"}},
		{"no question", `{"needs_human":true}`, "The create.elicit step cannot finish without a decision. What should it do?", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotQ string
			var gotOpts []string
			e := NewElicitorOn(&stageRecorder{outcome: stage.OutcomeNeedsHuman, output: tc.output}).WithHuman(
				func(_ context.Context, _ models.Task, q string, opts []string) error {
					gotQ, gotOpts = q, opts
					return nil
				})
			_, err := e.Analyze(context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "t"}}, models.Project{}, "")
			if !errors.Is(err, stage.ErrNeedsHuman) {
				t.Fatalf("Analyze() error = %v, want ErrNeedsHuman", err)
			}
			if gotQ != tc.wantQuestion || strings.Join(gotOpts, ",") != strings.Join(tc.wantOptions, ",") {
				t.Fatalf("asked %q %v, want %q %v", gotQ, gotOpts, tc.wantQuestion, tc.wantOptions)
			}
		})
	}
}
