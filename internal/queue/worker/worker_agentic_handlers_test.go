package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type respecFailGateway struct{}

func (g *respecFailGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	user := ""
	if n := len(req.Messages); n > 0 {
		user = req.Messages[n-1].Content
	}
	if strings.HasPrefix(user, "Revise the user task prompt") {
		return gateway.AIResponse{}, errors.New("respec gateway failure")
	}
	return gateway.AIResponse{Content: "repaired"}, nil
}

func (g *respecFailGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}
func (g *respecFailGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return &gateway.ScopeAnalysis{}, nil
}
func (g *respecFailGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return &gateway.IntentAnalysis{}, nil
}
func (g *respecFailGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func TestFinishAgenticTurnNoTools_RespecFailurePreservesMessages(t *testing.T) {
	t.Parallel()
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-respec-fail"}, ProjectID: "proj", AgentID: "agent"}
	cm := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, nil, task.AgentID, task.ID)
	committed := ""
	w := &Worker{
		store:         &mockCommitStore{text: &committed},
		gateway:       &respecFailGateway{},
		planningCfg:   config.AgenticPlanningConfig{ComplexityThreshold: 1, MaxRedoPasses: 0},
		messageEditor: NewMessageEditor(NewMemoryCheckpointStore(), nil, cm),
	}
	plan := &Plan{Steps: []PlanStep{{ID: "only", Action: "do", OutputFormat: "text"}}}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "<!-- step:only -->\n<!-- /step:only -->\n"},
	}
	before := len(messages)
	respecAttempts := 0
	cont, _, report, _, err := w.finishAgenticTurnNoTools(
		context.Background(),
		task,
		models.AgentProfile{},
		"<!-- step:only -->\n<!-- /step:only -->\n",
		plan,
		nil,
		"task-respec-fail:0",
		0,
		NewBudgetGuard(nil, task.ID),
		NewContextBudgetGuard(60000, 0),
		cm,
		&messages,
		&respecAttempts,
	)
	if err != nil {
		t.Fatalf("finishAgenticTurnNoTools: %v", err)
	}
	if cont {
		t.Fatal("expected no rewind when respec fails")
	}
	if !report {
		t.Fatal("expected report on fallback commit path")
	}
	if len(messages) != before {
		t.Fatalf("len(messages) = %d, want %d after failed respec", len(messages), before)
	}
}
