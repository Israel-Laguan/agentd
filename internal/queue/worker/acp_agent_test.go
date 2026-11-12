package worker

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"agentd/internal/agent/acp"
	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/sandbox/egress"
	"agentd/internal/stage"
	"agentd/internal/testutil"

	acpsdk "github.com/coder/acp-go-sdk"
)

// TestMain doubles as the fake ACP agent: `<test binary> fake-acp-agent`
// serves it on stdio, so ACPAgent drives a real process. It also registers the
// fake as preset "fake", and again as "quiet", which reports no usage. As
// `<test binary> jail-forward` it is agentd's forwarder in a live run's jail.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "jail-forward" {
		if err := egress.Forward(context.Background(), os.Stdout, os.Args[2:]); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == fakeagent.Arg {
		if err := fakeagent.Serve(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	fake := acp.Preset{Command: os.Args[0], Allow: []string{"model.test:443"},
		Home: func(acp.Launch) ([]string, error) { return []string{os.Args[0], fakeagent.Arg}, nil }}
	acp.Presets["fake"] = fake
	fake.NoUsage = true
	acp.Presets["quiet"] = fake
	os.Exit(m.Run())
}

// acpOut is the stage contract the fake agent reports against.
type acpOut struct {
	Name string `json:"name"`
}

func (o acpOut) Validate() error {
	if o.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// fakeACPAgent runs the PR 05 fake agent (TestMain) behind an ACPAgent for a
// writable stage, which most tests use.
func fakeACPAgent(t *testing.T, permit acp.Permitter) ACPAgent {
	t.Helper()
	return fakeACPAgentFor(t, permit, stage.AccessWrite)
}

// fakeACPAgentFor runs the fake agent behind a driver started for access, so a
// test can exercise read-only and no-workspace stages without a mismatch.
func fakeACPAgentFor(t *testing.T, permit acp.Permitter, access stage.WorkspaceAccess) ACPAgent {
	t.Helper()
	sessions := &agentmcp.SessionMux{}
	srv := httptest.NewServer(sessions)
	t.Cleanup(srv.Close)
	d := acp.NewDriver(acp.Config{
		Cwd:      t.TempDir(),
		ReadOnly: access != stage.AccessWrite,
		Command: func(ctx context.Context, _ string, _ []acpsdk.McpServer) (*exec.Cmd, error) {
			return exec.CommandContext(ctx, os.Args[0], fakeagent.Arg), nil
		},
	})
	t.Cleanup(func() { _ = d.Close() })
	return ACPAgent{Driver: d, Store: testutil.NewFakeStore(), ProjectID: "p1", Sessions: sessions, MCPURL: srv.URL, Permit: permit}
}

// runACPStage runs one stage whose context pack is the fake agent's script.
func runACPStage(t *testing.T, a ACPAgent, access stage.WorkspaceAccess, flagged bool, steps ...fakeagent.Step) (stage.StageResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.Call(ctx, stage.StageRequest{
		Stage:    stage.StageExecution,
		Purpose:  spec.PurposeExecTurn,
		Context:  []spec.PromptMessage{{Role: "user", Content: fakeagent.Script(steps...)}},
		Contract: stage.NewContract[acpOut](),
		Access:   access,
		Attrs:    spec.TaskAttrs{RequiresWorkspaceChange: &flagged},
	})
}

func report(result map[string]any) fakeagent.Step {
	return fakeagent.Step{Tool: &fakeagent.Tool{Name: "report_outcome", Args: map[string]any{"outcome": "completed", "summary": "done", "result": result}}}
}

var validReport = report(map[string]any{"name": "x"})

func TestACPAgentReportOutcomeIsTheStageResult(t *testing.T) {
	t.Parallel()
	res, err := runACPStage(t, fakeACPAgent(t, nil), stage.AccessWrite, false, validReport)
	if err != nil || res.Outcome != stage.OutcomeCompleted || string(res.Output) != `{"name":"x"}` {
		t.Fatalf("result = %q %s, %v; want completed with the reported result", res.Outcome, res.Output, err)
	}
}

// The schema error goes back to the agent, which reports again.
func TestACPAgentInvalidReportGetsTheSchemaErrorBack(t *testing.T) {
	t.Parallel()
	res, err := runACPStage(t, fakeACPAgent(t, nil), stage.AccessWrite, false, report(map[string]any{}), validReport)
	if err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("result = %q, %v; want the second, valid report", res.Outcome, err)
	}
}

func TestACPAgentRepairPromptRecoversAMissingOutcome(t *testing.T) {
	t.Parallel()
	res, err := runACPStage(t, fakeACPAgent(t, nil), stage.AccessWrite, false,
		fakeagent.Step{Message: "done"}, fakeagent.Step{Next: []fakeagent.Step{validReport}})
	if err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("result = %q, %v; want the outcome reported after the repair prompt", res.Outcome, err)
	}
}

// Only one repair prompt: the agent would report on a second one, so the call
// fails only if none is sent.
func TestACPAgentWithNoOutcomeAfterOneRepairFails(t *testing.T) {
	t.Parallel()
	res, err := runACPStage(t, fakeACPAgent(t, nil), stage.AccessWrite, false,
		fakeagent.Step{Message: "done"},
		fakeagent.Step{Next: []fakeagent.Step{{Message: "still done"}, {Next: []fakeagent.Step{validReport}}}})
	if !errors.Is(err, agentmcp.ErrNoOutcome) || res.Outcome == stage.OutcomeCompleted {
		t.Fatalf("result = %q, %v; want ErrNoOutcome and no completion", res.Outcome, err)
	}
}

func TestACPAgentAskHumanIsNeedsHumanWithTheQuestionKept(t *testing.T) {
	t.Parallel()
	ask := fakeagent.Step{Tool: &fakeagent.Tool{Name: "ask_human", Args: map[string]any{"question": "Which format did the client ask for?", "options": []string{"csv", "json"}}}}
	res, err := runACPStage(t, fakeACPAgent(t, nil), stage.AccessWrite, true, ask)
	if err != nil || res.Outcome != stage.OutcomeNeedsHuman {
		t.Fatalf("result = %q, %v; want needs_human", res.Outcome, err)
	}
	question, options := needsHumanQuestion(spec.PurposeExecTurn, res.Output)
	if question != "Which format did the client ask for?" || strings.Join(options, ",") != "csv,json" {
		t.Fatalf("handoff = %q %v; want the agent's question and options", question, options)
	}
}

// A flagged task's Completed needs evidence of work, as the builtin's B-082
// gate does; without it the task goes to a human, never to COMPLETED.
func TestACPAgentUnevidencedCompletionOfAFlaggedTaskIsUnverified(t *testing.T) {
	t.Parallel()
	a := fakeACPAgent(t, func(context.Context, acp.Permission) (acp.Verdict, error) { return acp.Verdict{Allow: true}, nil })
	res, err := runACPStage(t, a, stage.AccessWrite, true, validReport)
	if !errors.Is(err, stage.ErrUnverifiedCompletion) || res.Outcome == stage.OutcomeCompleted {
		t.Fatalf("flagged, no evidence: result = %q, %v; want ErrUnverifiedCompletion", res.Outcome, err)
	}
	if res, err := runACPStage(t, a, stage.AccessWrite, true, fakeagent.Step{Did: acpsdk.ToolKindEdit}, validReport); err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("flagged, with an edit: result = %q, %v; want completed", res.Outcome, err)
	}
	if res, err := runACPStage(t, a, stage.AccessWrite, false, validReport); err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("unflagged: result = %q, %v; want completed", res.Outcome, err)
	}
	if res, err := runACPStage(t, fakeACPAgentFor(t, nil, stage.AccessReadOnly), stage.AccessReadOnly, true, validReport); err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("read-only flagged: result = %q, %v; want completed without evidence", res.Outcome, err)
	}
}

// The real ACPPermitter refuses a sudo command through the whole ACP driver:
// the agent sees reject_once, not allow_once, and the turn ends on that refusal.
func TestACPAgentRefusesSudoThroughTheRealPermitter(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{ProjectName: "p", Tasks: []models.DraftTask{{Title: "sudo", Description: "d"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	w := NewWorker(store, nil, &batchCountingSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	taskHooks, _ := w.MountAgenticHooks(models.Project{}, models.AgentProfile{})
	a := fakeACPAgent(t, ACPPermitter(tasks[0], taskHooks, w.hooks))
	res, err := runACPStage(t, a, stage.AccessWrite, false,
		fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindExecute, RawInput: map[string]any{"command": "sudo id"}}},
		fakeagent.Step{Stop: acpsdk.StopReasonCancelled})
	if !errors.Is(err, acp.ErrPermissionRefused) {
		t.Fatalf("err = %v, want ErrPermissionRefused", err)
	}
	if !strings.Contains(res.Progress, "permission: reject_once") {
		t.Fatalf("progress = %q, want the agent to see reject_once", res.Progress)
	}
}

// A read-only stage's session refuses writes before the task's hooks.
func TestACPAgentReadOnlyStageRefusesWrites(t *testing.T) {
	t.Parallel()
	var asked []string
	permit := func(_ context.Context, p acp.Permission) (acp.Verdict, error) {
		asked = append(asked, p.Tool)
		return acp.Verdict{Allow: true}, nil
	}
	edit := fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindEdit, RawInput: map[string]any{"path": "a.go"}}}
	if _, err := runACPStage(t, fakeACPAgentFor(t, permit, stage.AccessReadOnly), stage.AccessReadOnly, false, edit, validReport); err != nil || len(asked) != 0 {
		t.Fatalf("read-only: err = %v, hooks asked for %v; want the edit refused before them", err, asked)
	}
	if _, err := runACPStage(t, fakeACPAgent(t, permit), stage.AccessWrite, false, edit, validReport); err != nil || strings.Join(asked, ",") != "write" {
		t.Fatalf("write: err = %v, hooks asked for %v; want the edit checked by them", err, asked)
	}
}

// A stage with no workspace access refuses every permission, including reads
// and commands, before the task's hooks.
func TestACPAgentAccessNoneRefusesEveryPermission(t *testing.T) {
	t.Parallel()
	var asked []string
	permit := func(_ context.Context, p acp.Permission) (acp.Verdict, error) {
		asked = append(asked, p.Tool)
		return acp.Verdict{Allow: true}, nil
	}
	a := fakeACPAgentFor(t, permit, stage.AccessNone)
	read := fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindRead, RawInput: map[string]any{"path": "a.go"}}}
	run := fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindExecute, RawInput: map[string]any{"command": "ls"}}}
	if res, err := runACPStage(t, a, stage.AccessNone, false, read, run, validReport); err != nil || res.Outcome != stage.OutcomeCompleted {
		t.Fatalf("result = %q, %v; want completed", res.Outcome, err)
	}
	if len(asked) != 0 {
		t.Fatalf("hooks asked for %v; want no permission to reach them", asked)
	}
}
