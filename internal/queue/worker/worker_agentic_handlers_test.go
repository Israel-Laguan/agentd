package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

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
	logs := logBuf.String()
	if !strings.Contains(logs, "agentic respec repair skipped") {
		t.Fatalf("expected respec repair skip warning, logs:\n%s", logs)
	}
	if !strings.Contains(logs, "task-respec-fail") || !strings.Contains(logs, "task-respec-fail:0") {
		t.Fatalf("expected task_id and turn_id in warning, logs:\n%s", logs)
	}
	if !strings.Contains(logs, "respec gateway failure") {
		t.Fatalf("expected respec error in warning, logs:\n%s", logs)
	}
}
