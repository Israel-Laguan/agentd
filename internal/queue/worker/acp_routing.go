package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"agentd/internal/agent/acp"
	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/gateway"
	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/queue/runaudit"
	"agentd/internal/stage"
)

// ACPRuntime is what the worker needs to run a stage on an acp agent (T-042).
type ACPRuntime struct {
	// Start prepares one run's jail for the agent at executable, which may
	// reach only allow. In agentd it is sandbox.BashExecutor.StartEgressRun.
	Start func(allow []string, executable string) (ACPRun, error)
	// Sessions serves each session's MCP server, which the agent reaches at MCPURL.
	Sessions *agentmcp.SessionMux
	MCPURL   string
	// Recorder gets one ai_calls record per call, as the gateway writes for a
	// model call. Nil records nothing.
	Recorder spec.Recorder
	cleanup  func() // ends the MCP server and removes its socket directory; nil when it owns none
}

// Close is the runtime's cleanup handle: it ends the MCP server and removes its
// socket directory, so a shutdown leaves nothing behind in TMPDIR. A runtime
// built by hand (tests) owns no server and Close is a no-op.
func (r *ACPRuntime) Close() error {
	if r.cleanup == nil {
		return nil
	}
	r.cleanup()
	return nil
}

// ACPRun is one agent run's jail: a fresh home, and the command that runs the
// agent in it (sandbox.BashExecutor.AgentCommand on the run's egress).
type ACPRun struct {
	Home    string
	Command func(ctx context.Context, cwd string, argv []string, readOnly bool) (*exec.Cmd, error)
	// Close ends the run's egress and removes Home, once the agent exited.
	Close func()
}

// noUsageMarker ends the model of a record whose agent reports no usage, so
// its zero tokens read as unknown, not free.
const noUsageMarker = " (no usage reported)"

// errNoACPRuntime is a route to an acp agent on a worker with no ACP runtime.
var errNoACPRuntime = errors.New("worker: the route names an acp agent, but no ACP runtime is wired")

// acpRoute resolves purpose and reports whether it routes to an acp agent. A
// pin keeps the model path, as it does in the gateway.
func (w *Worker) acpRoute(purpose spec.Purpose, agentID string, attrs spec.TaskAttrs) (route.Decision, route.Agent, bool) {
	d := w.routes.Resolve(purpose, agentID, attrs)
	a, ok := w.routes.Agents[d.Agent]
	return d, a, ok && d.Rule != route.RulePin && a.Kind == route.KindACP
}

// routedStageAgent picks the stage agent per call: the acp agent a route names,
// or the model agent. For a model call the gateway resolves the same table with
// the same purpose, agent and attributes, so it makes the same decision.
type routedStageAgent struct {
	w       *Worker
	model   stage.Agent
	task    models.Task
	project models.Project
}

// Call runs one stage on the routed agent: the acp agent the route names, or
// the model agent.
func (r routedStageAgent) Call(ctx context.Context, req stage.StageRequest) (stage.StageResult, error) {
	if req.AgentID == "" {
		req.AgentID = r.task.AgentID
	}
	if req.Attrs.RequiresWorkspaceChange == nil {
		req.Attrs = gateway.AttrsForTask(r.task.RequiresWorkspaceChange)
	}
	d, a, ok := r.w.acpRoute(req.Purpose, req.AgentID, req.Attrs)
	if !ok {
		return r.model.Call(ctx, req)
	}
	profile, err := r.w.store.GetAgentProfile(ctx, r.task.AgentID)
	if err != nil {
		return stage.StageResult{}, fmt.Errorf("worker: acp agent %q: load the task's profile: %w", d.Agent, err)
	}
	return r.w.callACP(ctx, r.task, r.project, *profile, d, a, req, nil, false)
}

// callACP runs one stage call on an acp agent with a driver of its own: a driver
// is jailed to one workspace and one access level, so it is never shared
// across tasks. Its permissions pass the task's hooks and the worker-global
// ones (the denylist), as a builtin tool call does.
// shortcut: every call cold-starts the agent; keep a warm driver per task if the start-up cost shows.
func (w *Worker) callACP(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, d route.Decision, a route.Agent, req stage.StageRequest, taskHooks *agenthooks.HookChain, sessionStarted bool) (stage.StageResult, error) {
	slog.InfoContext(ctx, "routing decision", "purpose", d.Purpose, "required_level", d.Required,
		"rule", d.Rule, "agent", d.Agent, "task_id", task.ID)
	budgetGuard := runaudit.NewBudgetGuard(w.budgetTracker, task.ID)
	// An attempt budget's caller has already reserved the call; an ACP call does
	// not run the correction loop, so charging its own guard here would double
	// count the response.
	if !correction.HasAttemptBudget(ctx) {
		if err := budgetGuard.BeforeCall(); err != nil {
			return stage.StageResult{}, err
		}
	}
	if w.acp == nil {
		return stage.StageResult{}, errNoACPRuntime
	}
	preset, ok := acp.Presets[a.Preset]
	if !ok {
		return stage.StageResult{}, fmt.Errorf("worker: acp agent %q: unknown preset %q", d.Agent, a.Preset)
	}
	driver, stop, err := w.acpDriver(preset, a, project.WorkspacePath, req.Access != stage.AccessWrite)
	if err != nil {
		return stage.StageResult{}, fmt.Errorf("worker: acp agent %q: %w", d.Agent, err)
	}
	defer stop()
	if !sessionStarted {
		taskHooks, _ = w.MountAgenticHooks(project, profile)
		if err := w.RunSessionStart(ctx, task, project, taskHooks); err != nil {
			return stage.StageResult{}, err
		}
	}
	agent := ACPAgent{
		Driver: driver, Store: w.store, ProjectID: project.ID,
		Sessions: w.acp.Sessions, MCPURL: w.acp.MCPURL,
		Permit: ACPPermitter(task, taskHooks, w.hooks),
	}
	started := time.Now()
	res, err := agent.Call(ctx, req)
	if !correction.ChargeAttemptBudget(ctx, res.TokenUsage) {
		budgetGuard.AfterCall(res.TokenUsage)
	}
	model := a.Preset
	if preset.NoUsage {
		model += noUsageMarker
	}
	w.recordACP(task, d, model, started, res, err)
	return res, err
}

// acpDriver starts one run's jail for a and returns its driver. stop closes
// the driver, which waits for the agent to exit, then the run.
func (w *Worker) acpDriver(preset acp.Preset, a route.Agent, cwd string, readOnly bool) (*acp.Driver, func(), error) {
	exe, err := preset.Executable(a.Executable)
	if err != nil {
		return nil, nil, err
	}
	run, err := w.acp.Start(preset.Allow, exe)
	if err != nil {
		return nil, nil, err
	}
	home, _ := os.UserHomeDir() // empty: no seed, and a relative login path that fails to read
	creds := a.Credentials
	if creds == "" {
		creds = filepath.Join(home, preset.Credentials)
	}
	launch := acp.Launch{Home: run.Home, Executable: exe, Model: a.Model, Credentials: creds, UserHome: home}
	driver := acp.NewDriver(preset.Config(launch, cwd, readOnly, run.Command))
	return driver, func() { _ = driver.Close(); run.Close() }, nil
}

// Close ends the ACP runtime's MCP server and removes its socket directory,
// when the worker has an acp agent; a worker with no acp agent has none and
// Close is a no-op.
func (w *Worker) Close() error {
	if w.acp == nil {
		return nil
	}
	return w.acp.Close()
}

// recordACP writes one ai_calls record for the call, as the gateway writes for
// a model call.
func (w *Worker) recordACP(task models.Task, d route.Decision, model string, started time.Time, res stage.StageResult, err error) {
	if w.acp.Recorder == nil {
		return
	}
	outcome := spec.OutcomeOK
	if err != nil {
		outcome = spec.OutcomeFailed
	}
	w.acp.Recorder.Record(spec.CallRecord{
		At: started, Kind: spec.KindGenerate, Purpose: d.Purpose, Stage: d.Purpose.Stage(),
		RoutedAgent: d.Agent, RoutedLevel: d.Required, RoutedRule: d.Rule,
		TaskID: task.ID, AgentID: task.AgentID, Provider: route.KindACP, Model: model,
		Attempt: spec.AttemptFirst, Outcome: outcome, OutputChars: len(res.Output),
		Tokens: res.TokenUsage, Latency: time.Since(started),
	})
}
