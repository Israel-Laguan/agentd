package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

// stageOut is a stage contract: it needs a name.
type stageOut struct {
	Name string `json:"name"`
}

func (o stageOut) Validate() error {
	if o.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (stageOut) SchemaHint() string { return `{"name": string}` }

func newTestSession(t *testing.T, store models.KanbanStore, projectID string) (*Session, *mcp.ClientSession) {
	t.Helper()
	s := NewSession(store, SessionConfig{ProjectID: projectID, Contract: stage.NewContract[stageOut](), Context: "the context pack"})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := s.mcpServer.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return s, cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = append(text, tc.Text)
		}
	}
	return strings.Join(text, "\n"), res.IsError
}

// A worker gets the contract tools and the board reads, never a board write.
func TestSessionOffersTheContractToolsAndOnlyBoardReads(t *testing.T) {
	t.Parallel()
	_, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	tools, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := "ask_human board.get_project board.get_task board.list_comments board.list_projects board.list_tasks get_context report_outcome"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("tools = %s; want %s", got, want)
	}
}

// The board reads see the session's project only, and a task or project of
// another one is not found.
func TestSessionBoardReadsStayInTheProject(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	own, ownTasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "own", Tasks: []models.DraftTask{{Title: "mine", AgentID: "default"}}})
	if err != nil {
		t.Fatal(err)
	}
	other, otherTasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "other", Tasks: []models.DraftTask{{Title: "theirs", AgentID: "default"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, cs := newTestSession(t, store, own.ID)

	if text, isErr := callTool(t, cs, "board.list_tasks", map[string]any{"project_id": other.ID}); isErr || strings.Contains(text, "theirs") || !strings.Contains(text, "mine") {
		t.Errorf("list_tasks naming the other project = %s; want only the session's tasks", text)
	}
	if text, isErr := callTool(t, cs, "board.list_projects", nil); isErr || strings.Contains(text, other.ID) || !strings.Contains(text, own.ID) {
		t.Errorf("list_projects = %s; want only the session's project", text)
	}
	if _, isErr := callTool(t, cs, "board.get_task", map[string]any{"task_id": ownTasks[0].ID}); isErr {
		t.Error("get_task of the session's own task failed")
	}
	for name, args := range map[string]map[string]any{
		"board.get_task":      {"task_id": otherTasks[0].ID},
		"board.list_comments": {"task_id": otherTasks[0].ID},
		"board.get_project":   {"project_id": other.ID},
	} {
		if text, isErr := callTool(t, cs, name, args); !isErr || strings.Contains(text, "theirs") {
			t.Errorf("%s of the other project = %s; want not found", name, text)
		}
	}
}

func TestReportOutcomeWithAValidResultIsTheStageResult(t *testing.T) {
	t.Parallel()
	s, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	if text, isErr := callTool(t, cs, "report_outcome", map[string]any{"outcome": "completed", "summary": "done", "result": map[string]any{"name": "x"}}); isErr {
		t.Fatalf("report_outcome = %s; want it accepted", text)
	}
	res, err := s.Result()
	if err != nil || res.Outcome != stage.OutcomeCompleted || string(res.Output) != `{"name":"x"}` {
		t.Fatalf("Result = %q %s, %v; want completed with the validated result", res.Outcome, res.Output, err)
	}
}

// An invalid result goes back to the agent with the contract's shape, so it
// can report again; nothing is recorded.
func TestReportOutcomeWithAnInvalidResultIsASchemaErrorToTheAgent(t *testing.T) {
	t.Parallel()
	s, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	for _, args := range []map[string]any{
		{"outcome": "completed", "summary": "done", "result": map[string]any{}},
		{"outcome": "completed", "summary": "done"},
		{"outcome": "finished", "summary": "done", "result": map[string]any{"name": "x"}},
	} {
		text, isErr := callTool(t, cs, "report_outcome", args)
		if !isErr || !strings.Contains(text, `{"name": string}`) {
			t.Errorf("report_outcome %v = %s (error %v); want a schema error naming the shape", args, text, isErr)
		}
	}
	if _, err := s.Result(); !errors.Is(err, ErrNoOutcome) {
		t.Fatalf("Result after invalid reports: %v; want ErrNoOutcome", err)
	}
}

func TestReportOutcomeFailedIsAnError(t *testing.T) {
	t.Parallel()
	s, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	callTool(t, cs, "report_outcome", map[string]any{"outcome": "failed", "summary": "the package does not exist"})
	if _, err := s.Result(); !errors.Is(err, ErrReportedFailure) || !strings.Contains(err.Error(), "the package does not exist") {
		t.Fatalf("Result = %v; want ErrReportedFailure with the summary", err)
	}
}

// ask_human is the stage seam's needs-human reply, and it is final: a guess
// reported after it does not replace the question.
func TestAskHumanIsNeedsHumanWithTheQuestionAndIsFinal(t *testing.T) {
	t.Parallel()
	s, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	if text, isErr := callTool(t, cs, "ask_human", map[string]any{"question": " Which format? ", "options": []string{"csv", " "}}); isErr {
		t.Fatalf("ask_human = %s", text)
	}
	if _, isErr := callTool(t, cs, "report_outcome", map[string]any{"outcome": "completed", "summary": "guessed", "result": map[string]any{"name": "x"}}); !isErr {
		t.Error("report_outcome after ask_human was accepted; want it refused")
	}
	if _, isErr := callTool(t, cs, "ask_human", map[string]any{"question": "Which encoding?"}); !isErr {
		t.Error("a second ask_human was accepted; want it refused")
	}
	res, err := s.Result()
	if err != nil || res.Outcome != stage.OutcomeNeedsHuman {
		t.Fatalf("Result = %q, %v; want needs_human", res.Outcome, err)
	}
	var out struct {
		NeedsHuman bool     `json:"needs_human"`
		Question   string   `json:"question"`
		Options    []string `json:"options"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil || !out.NeedsHuman || out.Question != "Which format?" || len(out.Options) != 1 {
		t.Fatalf("Output = %s; want the marked question with its one option", res.Output)
	}
	if _, isErr := callTool(t, cs, "ask_human", map[string]any{"question": " "}); !isErr {
		t.Error("ask_human with a blank question was accepted")
	}
}

func TestGetContextReturnsTheContextPack(t *testing.T) {
	t.Parallel()
	_, cs := newTestSession(t, testutil.NewFakeStore(), "p1")
	if text, isErr := callTool(t, cs, "get_context", nil); isErr || text != "the context pack" {
		t.Fatalf("get_context = %q; want the pack", text)
	}
}

// A session's server is reachable at its own path only while it runs.
func TestSessionMuxServesASessionOnlyAtItsPathUntilRemoved(t *testing.T) {
	t.Parallel()
	var m SessionMux
	path, remove := m.Add(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	status := func(p string) int {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, nil))
		return rec.Code
	}
	if status(path) != http.StatusTeapot || status("/mcp/other") != http.StatusNotFound {
		t.Fatalf("served %d at its path and %d elsewhere; want the session's handler only at its path", status(path), status("/mcp/other"))
	}
	if remove(); status(path) != http.StatusNotFound {
		t.Fatal("the session is still served after remove")
	}
}
