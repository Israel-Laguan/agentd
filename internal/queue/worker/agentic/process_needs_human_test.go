package agentic

import (
	"context"
	"fmt"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

// toolCapableGateway counts model calls and reports chat-tool support, so
// Process gets past the tool-support check.
type toolCapableGateway struct {
	generateCalls int
}

func (g *toolCapableGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	g.generateCalls++
	return gateway.AIResponse{Content: "done"}, nil
}

func (g *toolCapableGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func (g *toolCapableGateway) ProviderSupportsChatTools(string) bool { return true }

type planErrHost struct {
	*topicDriftHost
	planErr    error
	failedWith []error
	recorded   int
}

func (h *planErrHost) ShouldPlanWithBudget(models.Task, *agentruntime.BudgetGuard) bool { return true }

func (h *planErrHost) GeneratePlan(context.Context, models.Task, models.Project, *agentruntime.BudgetGuard) (*agentcontext.Plan, error) {
	return nil, h.planErr
}

func (h *planErrHost) FailHard(_ context.Context, _ models.Task, err error) {
	h.failedWith = append(h.failedWith, err)
}

func (h *planErrHost) RecordLoopResult(agentruntime.LoopResult) { h.recorded++ }

func runProcessWithPlanErr(t *testing.T, planErr error) (*planErrHost, *toolCapableGateway, bool) {
	t.Helper()
	gw := &toolCapableGateway{}
	host := &planErrHost{topicDriftHost: &topicDriftHost{noopHost: &noopHost{}, committed: new(string)}, planErr: planErr}
	e := NewEngine(Config{Store: testutil.NewFakeStore(), Gateway: gw}, host)
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, ProjectID: "project-1", AgentID: "agent-1", Title: "t"}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	_, ok := e.Process(context.Background(), task, project, profile)
	return host, gw, ok
}

func TestProcessStopsWithoutTurnLoopWhenWorkPlanNeedsHuman(t *testing.T) {
	host, gw, ok := runProcessWithPlanErr(t, fmt.Errorf("%w: exec.work_plan", stage.ErrNeedsHuman))
	if ok {
		t.Fatal("Process reported a result; the task is blocked on a human")
	}
	if gw.generateCalls != 0 || host.recorded != 0 {
		t.Fatalf("turn loop ran: model calls=%d recorded=%d", gw.generateCalls, host.recorded)
	}
	if len(host.failedWith) != 0 {
		t.Fatalf("FailHard called for a blocked task: %v", host.failedWith)
	}
}

func TestProcessFailsHardWhenWorkPlanHandoffFails(t *testing.T) {
	host, gw, ok := runProcessWithPlanErr(t, fmt.Errorf("%w: exec.work_plan", stage.ErrHumanHandoffFailed))
	if ok || gw.generateCalls != 0 {
		t.Fatalf("turn loop ran after a failed handoff: ok=%v calls=%d", ok, gw.generateCalls)
	}
	if len(host.failedWith) != 1 {
		t.Fatalf("FailHard calls = %d, want 1", len(host.failedWith))
	}
}
