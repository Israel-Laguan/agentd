//go:build live

package worker

// The T-042 / SP-024 comparison: the three SP-025 tasks on each acp preset in
// its egress jail, and on the builtin agentic agent, both through the worker's
// execution seam and the bubblewrap jail. Every workspace is checked on the
// host against its seed; the agent's report is never the evidence. Never in
// CI: every run is a real model call. Ask the owner before each run.
//
//	AGENTD_LIVE_ACP=opencode,cline         presets to run
//	AGENTD_LIVE_MODEL_OPENCODE=...         a free model (default below); _CLINE likewise
//	AGENTD_LIVE_BUILTIN=1                  also run the builtin agent on codestral
//	AGENTD_LIVE_API_KEY_ENV=MISTRAL_API_KEY  the builtin's key; AGENTD_LIVE_BASE_URL, AGENTD_LIVE_MODEL
//	AGENTD_LIVE_TASKS=small,multi,ambiguous
//	AGENTD_LIVE_RUNS=3                     runs per task
//	AGENTD_LIVE_DELAY=8s                   pause between runs (provider rate limits)

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway/route"
	"agentd/internal/gateway/routing"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

var liveModels = map[string]string{
	"opencode": "opencode/deepseek-v4.1-flash-free",
	"cline":    "nvidia/nemotron-3-ultra-550b-a55b:free",
}

func TestLiveACPPresetsAgainstTheBuiltinAgent(t *testing.T) {
	presets := liveList("AGENTD_LIVE_ACP", "")
	builtin := os.Getenv("AGENTD_LIVE_BUILTIN") == "1"
	if len(presets) == 0 && !builtin {
		t.Skip("set AGENTD_LIVE_ACP=opencode,cline and/or AGENTD_LIVE_BUILTIN=1")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap is required: an agent runs only in the jail")
	}
	daemons := clineHubDaemons()
	// The hub daemon starts inside the jail and must die with its PID namespace.
	defer func() {
		if left := clineHubDaemons(); left > daemons {
			t.Errorf("%d cline hub daemons on the host after the runs, %d before", left, daemons)
		}
	}()
	runs := 3
	if _, err := fmt.Sscan(os.Getenv("AGENTD_LIVE_RUNS"), &runs); err != nil {
		runs = 3
	}
	delay, _ := time.ParseDuration(os.Getenv("AGENTD_LIVE_DELAY"))
	var results []liveResult
	for _, task := range liveTasks(liveList("AGENTD_LIVE_TASKS", "small,multi,ambiguous")) {
		for i := range runs {
			for _, p := range presets {
				model := os.Getenv("AGENTD_LIVE_MODEL_" + strings.ToUpper(p))
				if model == "" {
					model = liveModels[p]
				}
				routes := route.Table{
					Agents: map[string]route.Agent{"coder": {Kind: route.KindACP, Preset: p, Model: model, Level: spec.LevelL3}},
					Stages: map[spec.Purpose]route.StageRoute{spec.PurposeExecTurn: {Default: "coder"}},
				}
				results = append(results, liveRun(t, p, task, i, func(store models.KanbanStore, ex *sandbox.BashExecutor, sink models.EventSink) *Worker {
					rt, err := NewACPRuntime(routes, ex, os.Args[0], nil)
					if err != nil {
						t.Fatal(err)
					}
					return NewWorker(store, nil, ex, nil, sink, WorkerOptions{Routes: routes, ACP: rt})
				}))
				time.Sleep(delay)
			}
			if builtin {
				results = append(results, liveRun(t, "builtin", task, i, liveBuiltin(t)))
				time.Sleep(delay)
			}
		}
	}
	t.Log("\n" + liveTable(results))
}

// liveBuiltin builds the builtin agent's worker with the daemon's default
// budgets: B-089 is fixed (PR 02), so the default agentic budget is measured.
func liveBuiltin(t *testing.T) func(models.KanbanStore, *sandbox.BashExecutor, models.EventSink) *Worker {
	keyEnv := envDefault("AGENTD_LIVE_API_KEY_ENV", "MISTRAL_API_KEY")
	key := os.Getenv(keyEnv)
	if key == "" {
		t.Fatalf("AGENTD_LIVE_BUILTIN=1 needs %s", keyEnv)
	}
	cfg, err := config.Load(config.LoadOptions{HomeOverride: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := routing.NewRouterFromConfigs([]spec.ProviderConfig{{Name: "live", Adapter: "openai",
		BaseURL: envDefault("AGENTD_LIVE_BASE_URL", "https://api.mistral.ai/v1"), APIKey: key,
		Model: envDefault("AGENTD_LIVE_MODEL", "codestral-latest"), Timeout: 5 * time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	gw.WithTruncation(cfg.Gateway.TruncatorImpl(gw, nil), cfg.Gateway.Truncator.MaxInputChars)
	return func(store models.KanbanStore, ex *sandbox.BashExecutor, sink models.EventSink) *Worker {
		return NewWorker(store, gw, ex, nil, sink, WorkerOptions{
			MaxToolIterations: cfg.Queue.MaxToolIterations, AgenticTruncatorMax: cfg.Queue.AgenticTruncatorMax,
			AgenticCharacterBudget: config.EffectiveAgenticCharacterBudget(cfg.Queue.AgenticCharacterBudget, cfg.Gateway.Truncator.MaxInputChars),
			AgenticContext:         cfg.Queue.AgenticContext, ContextTruncation: cfg.Gateway.Truncation.StrategyImpl(),
		})
	}
}

// liveRun seeds a workspace, runs task on the worker side builds, and checks
// the workspace on the host. A refused agentd MCP tool shows as a run with no
// outcome: report_outcome is how the run ends.
func liveRun(t *testing.T, side string, task liveTask, i int, build func(models.KanbanStore, *sandbox.BashExecutor, models.EventSink) *Worker) liveResult {
	t.Helper()
	store, running, project, profile := acpTask(t, task.title, task.description, !task.ambiguous)
	if side == "builtin" {
		profile.AgenticMode, profile.Provider, profile.Model = true, "live", ""
		if err := store.UpsertAgentProfile(context.Background(), profile); err != nil {
			t.Fatal(err)
		}
	}
	seedWorkspace(t, project.WorkspacePath)
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	ex := &sandbox.BashExecutor{Root: filepath.Dir(project.WorkspacePath), Backend: "bubblewrap",
		ToolchainPaths: []string{strings.TrimSpace(string(goroot))}}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	sink := &mockEventSink{}
	build(store, ex, sink).dispatchProcess(ctx, running, project, profile)
	got, err := store.GetTask(context.Background(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := checkWorkspace(t, project.WorkspacePath, task)
	r.side, r.task, r.run, r.state, r.latency = side, task.name, i+1, got.State, time.Since(started)
	for _, ev := range sink.events[max(0, len(sink.events)-3):] {
		r.events = append(r.events, string(ev.Type)+": "+truncate(ev.Payload, 300))
	}
	want := models.TaskStateCompleted
	if task.ambiguous {
		want = models.TaskStateBlocked // NeedsHuman: the question is with a human
	}
	if r.state != want || !r.pass() {
		t.Errorf("%s %s run %d: %+v", side, task.name, i+1, r)
	}
	return r
}

// clineHubDaemons counts cline hub daemons on the host. The bracket keeps
// pgrep from matching its own command line (docs/acp-agents.md pitfalls).
func clineHubDaemons() int {
	out, _ := exec.Command("pgrep", "-f", "cline-hub-daemo[n]").Output()
	return len(strings.Fields(string(out)))
}

func liveList(name, fallback string) []string {
	var out []string
	for _, item := range strings.Split(envDefault(name, fallback), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func envDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
