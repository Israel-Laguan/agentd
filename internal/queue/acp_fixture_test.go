package queue

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"agentd/internal/agent/acp"
	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// TestMain doubles as the fake ACP agent: `<test binary> fake-acp-agent` serves
// it on stdio, so a queue test drives a real agent process. It also registers the
// fake as preset "fake", which is what acpQueueRoutes names.
//
// The queue tests used to run every task on the builtin loop, through
// queueGateway and queueSandbox. After T-070 the coordinator has no builtin
// branch, so a task reaches an agent: these tests drive the fake agent and keep
// asserting the coordinator's own behaviour — what it does with the outcome.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == fakeagent.Arg {
		if err := fakeagent.Serve(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	acp.Presets["fake"] = acp.Preset{
		Command: os.Args[0],
		Home:    func(acp.Launch) ([]string, error) { return []string{os.Args[0], fakeagent.Arg}, nil },
	}
	os.Exit(m.Run())
}

// acpQueueRoutes sends exec.turn to the fake agent; every other purpose falls
// back to the gateway the worker was built with, which is the L1 path the
// coordinator still owns (intake, clarify, split).
func acpQueueRoutes() route.Table {
	return route.Table{
		Agents: map[string]route.Agent{
			"coder": {Kind: route.KindACP, Preset: "fake", Level: spec.LevelL3},
		},
		Stages: map[spec.Purpose]route.StageRoute{
			spec.PurposeExecTurn: {Default: "coder"},
		},
	}
}

// acpQueueRuntime runs the registered fake preset unjailed. The scenario steps
// build workers outside a *testing.T, so the run's home is a temporary directory
// the run itself removes.
func acpQueueRuntime(t *testing.T) *ACPRuntime {
	t.Helper()
	rt, closeServer := newACPRuntime()
	t.Cleanup(closeServer)
	return rt
}

func newACPRuntime() (*ACPRuntime, func()) {
	sessions := &agentmcp.SessionMux{}
	srv := httptest.NewServer(sessions)
	return &ACPRuntime{
		Start: func(_ []string, _ string) (ACPRun, error) {
			home, err := os.MkdirTemp("", "agentd-acp-home")
			if err != nil {
				return ACPRun{}, err
			}
			return ACPRun{
				Home: home,
				Command: func(ctx context.Context, _ string, argv []string, _ bool) (*exec.Cmd, error) {
					return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
				},
				Close: func() { _ = os.RemoveAll(home) },
			}, nil
		},
		Sessions: sessions, MCPURL: srv.URL,
	}, srv.Close
}

// newObservedACPRuntime is the fake agent's runtime with two signals a scenario
// can wait on: a prompt started, and the run's context was cancelled, which is
// how the reaper's deadline reaches the agent process.
func newObservedACPRuntime(started, stopped chan struct{}) (*ACPRuntime, func()) {
	var onceStart, onceStop sync.Once
	sessions := &agentmcp.SessionMux{}
	srv := httptest.NewServer(sessions)
	rt := &ACPRuntime{
		Start: func(_ []string, _ string) (ACPRun, error) {
			home, err := os.MkdirTemp("", "agentd-acp-home")
			if err != nil {
				return ACPRun{}, err
			}
			return ACPRun{
				Home: home,
				Command: func(ctx context.Context, _ string, argv []string, _ bool) (*exec.Cmd, error) {
					onceStart.Do(func() { close(started) })
					cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
					cmd.Cancel = func() error {
						onceStop.Do(func() { close(stopped) })
						return cmd.Process.Kill()
					}
					return cmd, nil
				},
				Close: func() { _ = os.RemoveAll(home) },
			}, nil
		},
		Sessions: sessions, MCPURL: srv.URL,
	}
	return rt, srv.Close
}

// newPanicACPRuntime panics where the run starts, which is where the builtin
// loop's panicking sandbox used to panic from: inside the dispatch call the
// worker recovers.
func newPanicACPRuntime() *ACPRuntime {
	return &ACPRuntime{Start: func([]string, string) (ACPRun, error) {
		panic("intentional test panic from the agent runtime")
	}}
}

// newACPWorkerStore is a worker store whose project workspace is a temporary
// directory, so the agent has somewhere to run.
func newACPWorkerStore(t *testing.T) *workerStore {
	t.Helper()
	store := newWorkerStore()
	store.project.WorkspacePath = t.TempDir()
	return store
}

// newACPWorker builds a worker that runs exec.turn on the fake agent. gw answers
// the L1 calls the coordinator still makes (clarify, split); sb is unused on this
// path and stays for the tests that also assert the loop's sandbox.
func newACPWorker(t *testing.T, store *workerStore, gw gateway.AIGateway, sb sandbox.Executor, sink models.EventSink, opts WorkerOptions) *Worker {
	t.Helper()
	opts.Routes = acpQueueRoutes()
	opts.ACP = acpQueueRuntime(t)
	return NewWorker(store, gw, sb, NewCircuitBreaker(), sink, opts)
}

// scriptTask puts steps in the task's title, which leads the exec.turn context
// pack: the fake agent reads its script from the prompt, so no test shares state.
func scriptTask(store *workerStore, steps ...fakeagent.Step) {
	store.task.Title = fakeagent.Script(steps...)
}

// acpReport is a step that completes the task through report_outcome, with the
// exec.turn contract's summary.
func acpReport(summary string) fakeagent.Step {
	return fakeagent.Step{Tool: &fakeagent.Tool{Name: "report_outcome", Args: map[string]any{
		"outcome": "completed", "summary": summary, "result": map[string]any{"summary": summary},
	}}}
}
