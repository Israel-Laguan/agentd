package worker

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/agent/acp"
	"agentd/internal/agent/acp/fakeagent"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"

	acpsdk "github.com/coder/acp-go-sdk"
)

type recordSink struct {
	mu   sync.Mutex
	recs []spec.CallRecord
}

func (s *recordSink) Record(r spec.CallRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, r)
}

func (s *recordSink) all() []spec.CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]spec.CallRecord(nil), s.recs...)
}

// acpRoutes sends exec.turn to the fake acp agent coder, and a flagged task's
// create.split_task to it with the level raised to L3; quick, a model agent,
// takes the rest of create.split_task.
func acpRoutes() route.Table {
	flagged := true
	return route.Table{
		Agents: map[string]route.Agent{
			"quick": {Kind: route.KindModel, Provider: "fake", Level: spec.LevelL1},
			"coder": {Kind: route.KindACP, Preset: "fake", Level: spec.LevelL3},
		},
		Stages: map[spec.Purpose]route.StageRoute{
			spec.PurposeExecTurn: {Default: "coder"},
			spec.PurposeCreateSplitTask: {Default: "quick", Rules: []route.Rule{
				{Match: route.Match{RequiresWorkspaceChange: &flagged}, Use: "coder", MinLevel: spec.LevelL3},
			}},
		},
	}
}

// acpRuntime runs the registered fake preset unjailed and records its calls.
type acpReadOnlyLog struct {
	mu   sync.Mutex
	args []bool
}

func (l *acpReadOnlyLog) record(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.args = append(l.args, v)
}

func (l *acpReadOnlyLog) all() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bool(nil), l.args...)
}

func acpRuntime(t *testing.T) (*ACPRuntime, *recordSink, *acpReadOnlyLog) {
	t.Helper()
	sessions := &agentmcp.SessionMux{}
	srv := httptest.NewServer(sessions)
	t.Cleanup(srv.Close)
	sink := &recordSink{}
	log := &acpReadOnlyLog{}
	return &ACPRuntime{
		Command: func(ctx context.Context, _ string, argv []string, readOnly bool) (*exec.Cmd, error) {
			log.record(readOnly)
			return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
		},
		Sessions: sessions, MCPURL: srv.URL, Recorder: sink,
	}, sink, log
}

// acpFixture is a task in a store with a worker on acpRoutes; title is the
// task's title, which leads the exec.turn context pack the fake reads its
// script from.
func acpFixture(t *testing.T, title string, flagged bool) (*Worker, *testutil.FakeKanbanStore, models.Task, models.Project, models.AgentProfile, *recordSink, *acpReadOnlyLog) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(t.TempDir())
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "acp", Description: "d",
		Tasks: []models.DraftTask{{Title: title, Description: "do the thing", RequiresWorkspaceChange: flagged}}})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	project, err := store.GetProject(ctx, running.ProjectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	profile, err := store.GetAgentProfile(ctx, running.AgentID)
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	rt, sink, roLog := acpRuntime(t)
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &mockEventSink{}, WorkerOptions{Routes: acpRoutes(), ACP: rt})
	return w, store, *running, *project, *profile, sink, roLog
}

func TestResolveExecutionAgentPicksTheACPAgentAnExecTurnRouteNames(t *testing.T) {
	t.Parallel()
	w, _, task, project, profile, _, _ := acpFixture(t, "work", false)
	got := w.resolveExecutionAgent(context.Background(), task, project, profile)
	if got.kind != executionAgentACP || got.route.Agent != "coder" || got.agent.Preset != "fake" || got.route.Required != spec.LevelL3 {
		t.Fatalf("resolution = %+v, want the acp agent coder at L3", got)
	}
	// With no acp agent configured, the task stays on the builtin loop.
	w.routes = route.Table{}
	if got := w.resolveExecutionAgent(context.Background(), task, project, profile); got.kind != executionAgentAgentic {
		t.Fatalf("resolution with no routes = %q, want %q", got.kind, executionAgentAgentic)
	}
}

func TestACPExecutionCompletesTheTaskWithTheReportedSummary(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "added the endpoint and ran go test"}))
	w, store, task, project, profile, sink, roLog := acpFixture(t, script, false)
	w.dispatchProcess(context.Background(), task, project, profile)

	got, err := store.GetTask(context.Background(), task.ID)
	if err != nil || got.State != models.TaskStateCompleted {
		t.Fatalf("task = %+v, %v; want COMPLETED", got, err)
	}
	events, err := store.ListEventsByTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	var resultPayload string
	for _, ev := range events {
		if ev.Type == models.EventTypeResult {
			resultPayload = ev.Payload
		}
	}
	if !strings.Contains(resultPayload, "added the endpoint and ran go test") {
		t.Fatalf("committed result payload = %q, want it to contain the reported summary", resultPayload)
	}
	recs := sink.all()
	if len(recs) != 1 || recs[0].Purpose != spec.PurposeExecTurn || recs[0].RoutedAgent != "coder" || recs[0].Provider != route.KindACP || recs[0].Outcome != spec.OutcomeOK {
		t.Fatalf("records = %+v, want one ok exec.turn record for coder", recs)
	}
	if got := roLog.all(); len(got) != 1 || got[0] {
		t.Fatalf("acp Command readOnly args = %v, want [false] for a writable exec.turn", got)
	}
}

// B-082: a flagged task's completion with no evidence of work goes to a human.
func TestACPExecutionUnverifiedCompletionFailsToAHuman(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, _ := acpFixture(t, script, true)
	w.dispatchProcess(context.Background(), task, project, profile)

	got, err := store.GetTask(context.Background(), task.ID)
	if err != nil || got.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("task = %+v, %v; want FAILED_REQUIRES_HUMAN", got, err)
	}
}

// The raise-level rule: a flagged task's create.split_task runs on the L3 acp
// agent, and its record carries the rule and the raised level; an unflagged
// one stays on the model agent at L1.
func TestRaisedLevelSendsAFlaggedSplitToTheACPAgent(t *testing.T) {
	t.Parallel()
	w, _, task, project, _, sink, roLog := acpFixture(t, "split me", true)
	model := &stageRecorder{output: `{"name":"model"}`}
	w.stageAgent = model
	ctx, cancel := context.WithTimeout(gateway.WithTaskID(context.Background(), task.ID), 30*time.Second)
	defer cancel()
	split := func(flagged bool) stage.StageRequest {
		return stage.StageRequest{
			Stage: stage.StageTaskCreation, Purpose: gateway.PurposeCreateSplitTask,
			Context:  []spec.PromptMessage{{Role: "user", Content: fakeagent.Script(validReport)}},
			Contract: stage.NewContract[acpOut](), AgentID: task.AgentID, Attrs: gateway.AttrsForTask(flagged),
		}
	}

	res, err := w.taskCreationAgent(task, project).Call(ctx, split(true))
	if err != nil || string(res.Output) != `{"name":"x"}` {
		t.Fatalf("flagged split = %s, %v; want the acp agent's report", res.Output, err)
	}
	recs := sink.all()
	if len(recs) != 1 || recs[0].Purpose != spec.PurposeCreateSplitTask || recs[0].RoutedAgent != "coder" ||
		recs[0].RoutedLevel != spec.LevelL3 || recs[0].RoutedRule != "requires_workspace_change=true" || recs[0].Model != "fake" {
		t.Fatalf("records = %+v, want coder at the raised L3 by its rule", recs)
	}
	if got := roLog.all(); len(got) != 1 || !got[0] {
		t.Fatalf("acp Command readOnly args = %v, want [true] for a create.split_task stage without write access", got)
	}

	if _, err := w.taskCreationAgent(task, project).Call(ctx, split(false)); err != nil {
		t.Fatalf("unflagged split: %v", err)
	}
	if len(model.reqs) != 1 || len(sink.all()) != 1 {
		t.Fatalf("model calls = %d, acp records = %d; want the unflagged split on the model agent only", len(model.reqs), len(sink.all()))
	}
}

func TestACPRouteWithNoRuntimeFails(t *testing.T) {
	t.Parallel()
	w, _, task, project, _, _, _ := acpFixture(t, "split me", true)
	w.acp = nil
	_, err := w.taskCreationAgent(task, project).Call(context.Background(), stage.StageRequest{
		Stage: stage.StageTaskCreation, Purpose: gateway.PurposeCreateSplitTask,
		Contract: stage.NewContract[acpOut](), Attrs: gateway.AttrsForTask(true),
	})
	if err == nil || !strings.Contains(err.Error(), "no ACP runtime") {
		t.Fatalf("err = %v, want the missing runtime named", err)
	}
}

// The PR 05 review: an ACP permission passes the worker-global chain (the
// denylist) as well as the task's, as a builtin tool call does. A nil task
// chain means "use the global chain"; a non-nil allowing task chain must not
// mask a refusing global chain.
func TestACPPermitterRunsTheWorkerGlobalChain(t *testing.T) {
	t.Parallel()
	global := agenthooks.NewHookChain()
	global.RegisterPre(agenthooks.DenylistHook(t.TempDir()))
	permit := ACPPermitter(models.Task{ProjectID: "p1"}, nil, global)
	v, err := permit(context.Background(), acp.Permission{ToolCallID: "c1", Tool: "bash", Args: `{"command":"sudo rm -rf /"}`, Command: "sudo rm -rf /"})
	if err != nil || v.Allow {
		t.Fatalf("verdict = %+v, %v; want the denylist to refuse it", v, err)
	}

	allowing := agenthooks.NewHookChain()
	permit = ACPPermitter(models.Task{ProjectID: "p1"}, allowing, global)
	v, err = permit(context.Background(), acp.Permission{ToolCallID: "c1", Tool: "bash", Args: `{"command":"sudo rm -rf /"}`, Command: "sudo rm -rf /"})
	if err != nil || v.Allow {
		t.Fatalf("verdict with an allowing task chain = %+v, %v; want the denylist to still refuse it", v, err)
	}
}

// An ACP exec.turn writes the same task lifecycle audit records as the
// builtin agentic path: task_start then task_complete.
func TestACPExecutionAuditsStartAndComplete(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "added the endpoint and ran go test"}))
	w, _, task, project, profile, _, _ := acpFixture(t, script, false)
	path := withAuditFile(t, w)

	w.dispatchProcess(context.Background(), task, project, profile)

	got := auditTypesFor(t, path, task.ID)
	if len(got) != 2 || got[0] != "task_start" || got[1] != "task_complete" {
		t.Fatalf("task audit records = %v, want [task_start task_complete]", got)
	}
}

// A result that cannot be persisted must not be audited as task_complete.
func TestACPExecutionAuditsFailWhenTheResultCannotBePersisted(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, _ := acpFixture(t, script, false)
	w.store = &failingResultStore{FakeKanbanStore: store}
	path := withAuditFile(t, w)

	w.dispatchProcess(context.Background(), task, project, profile)

	got := auditTypesFor(t, path, task.ID)
	if len(got) != 2 || got[0] != "task_start" || got[1] != "task_fail" {
		t.Fatalf("task audit records = %v, want [task_start task_fail]", got)
	}
}

// exec.work_plan can route to an ACP agent. The response is charged to the
// caller's attempt budget once: ACP never runs the correction loop, so the call
// must not also charge its own guard and GeneratePlan's fallback.
func TestACPWorkPlanChargesTheBudgetOnce(t *testing.T) {
	t.Parallel()
	step := report(map[string]any{"steps": []any{map[string]any{
		"id": "analyze", "action": "read the code",
	}}})
	step.Usage = &acpsdk.Usage{TotalTokens: 321}
	w, _, task, project, _, _, _ := acpFixture(t, "plan the work", false)
	task.Description = fakeagent.Script(step)
	w.routes.Stages[spec.PurposeExecWorkPlan] = route.StageRoute{Default: "coder"}
	tracker := gateway.NewBudgetTracker(10000)
	guard := agentruntime.NewBudgetGuard(tracker, task.ID)

	plan, err := w.GeneratePlan(context.Background(), task, project, guard)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "analyze" {
		t.Fatalf("plan = %+v, want the one analyze step", plan)
	}
	if got := guard.Usage(); got != 321 {
		t.Fatalf("budget usage = %d, want 321: the ACP response must be charged once", got)
	}
}

type refusingBudgetTracker struct{ reserves int }

func (b *refusingBudgetTracker) Reserve(string) error {
	b.reserves++
	return models.ErrBudgetExceeded
}
func (b *refusingBudgetTracker) Add(string, int)  {}
func (b *refusingBudgetTracker) Usage(string) int { return 0 }
func (b *refusingBudgetTracker) Reset(string)     {}

// An ACP exec.turn reserves the task token budget before the agent starts.
func TestACPExecutionReservesTaskBudget(t *testing.T) {
	t.Parallel()
	script := fakeagent.Script(report(map[string]any{"summary": "summary"}))
	w, store, task, project, profile, sink, _ := acpFixture(t, script, false)
	tracker := &refusingBudgetTracker{}
	w.budgetTracker = tracker

	w.dispatchProcess(context.Background(), task, project, profile)

	if tracker.reserves == 0 {
		t.Fatal("task budget was never reserved for the ACP call")
	}
	got, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State == models.TaskStateCompleted {
		t.Fatal("task completed despite an exhausted task token budget")
	}
	if recs := sink.all(); len(recs) != 0 {
		t.Fatalf("acp call records = %+v, want none: the agent must not start over budget", recs)
	}
}
