package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func newTestServer(t *testing.T) (*Server, *testutil.FakeKanbanStore) {
	t.Helper()
	store := testutil.NewFakeStore()
	s := New(store)
	return s, store
}

func TestServer_ListTasks_Empty(t *testing.T) {
	s, _ := newTestServer(t)
	assert.NotNil(t, s.mcpServer)
}

func TestServer_ToolsRegistered(t *testing.T) {
	s, _ := newTestServer(t)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	tools, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}

	assert.True(t, names["board.list_tasks"])
	assert.True(t, names["board.get_task"])
	assert.True(t, names["board.list_projects"])
	assert.True(t, names["board.get_project"])
	assert.True(t, names["board.list_comments"])
	assert.True(t, names["board.add_comment"])
	assert.True(t, names["board.update_task_state"])
	assert.True(t, names["board.assign_task"])
}

func TestServer_ListTasks_WithData(t *testing.T) {
	s, store := newTestServer(t)

	project, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "test-project",
		Tasks: []models.DraftTask{
			{Title: "task-1", AgentID: "default"},
		},
	})
	require.NoError(t, err)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "board.list_tasks",
		Arguments: map[string]any{"project_id": project.ID},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Content)
}

// callListTasks is a small helper that calls board.list_tasks and decodes the
// task array from the tool result's text payload.
func callListTasks(t *testing.T, ctx context.Context, session *mcp.ClientSession, args map[string]any) []map[string]any {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "board.list_tasks",
		Arguments: args,
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "board.list_tasks returned a tool error")
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var tasks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &tasks))
	return tasks
}

// TestServer_ListTasks_BoardWideExposesAllTasks pins B-005: the board-wide
// call must not silently cap at 100 tasks. A client exporting a >100-task board
// has to be able to get every task — either because the default page is large
// enough or because limit/offset page through the whole board.
func TestServer_ListTasks_BoardWideExposesAllTasks(t *testing.T) {
	s, store := newTestServer(t)

	const taskCount = 150
	drafts := make([]models.DraftTask, taskCount)
	for i := range drafts {
		drafts[i] = models.DraftTask{Title: fmt.Sprintf("task-%03d", i), AgentID: "default"}
	}
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "big-project",
		Tasks:       drafts,
	})
	require.NoError(t, err)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	ctx := context.Background()

	// Default call: a 150-task board must come back whole, not truncated at 100.
	all := callListTasks(t, ctx, session, nil)
	assert.Len(t, all, taskCount, "default board-wide call must return every task, not a silent 100-task page")

	// Explicit limit at least as large as the board returns everything.
	whole := callListTasks(t, ctx, session, map[string]any{"limit": taskCount})
	assert.Len(t, whole, taskCount, "an explicit limit must be honoured, not clamped to a hidden 100")

	// limit + offset page through the board in two halves.
	first := callListTasks(t, ctx, session, map[string]any{"limit": 100, "offset": 0})
	require.Len(t, first, 100)
	second := callListTasks(t, ctx, session, map[string]any{"limit": 100, "offset": 100})
	assert.Len(t, second, taskCount-100, "offset must page past the first page")
}

// TestServer_ListTasks_StateFilterComposesWithProject pins B-006: the state
// filter must be honoured when project_id is also passed. A client asking for
// "the failed tasks in this project" must not get the whole project back.
func TestServer_ListTasks_StateFilterComposesWithProject(t *testing.T) {
	s, store := newTestServer(t)

	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "mixed-project",
		Tasks: []models.DraftTask{
			{Title: "done-1", AgentID: "default"},
			{Title: "done-2", AgentID: "default"},
		},
	})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		_, err := store.UpdateTaskState(context.Background(), task.ID, task.UpdatedAt, models.TaskStateCompleted)
		require.NoError(t, err)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	ctx := context.Background()

	// state=FAILED on a project of COMPLETED tasks: none, with project_id.
	failedInProject := callListTasks(t, ctx, session, map[string]any{"project_id": project.ID, "state": "FAILED"})
	assert.Empty(t, failedInProject, "state=FAILED must return no tasks from a project of COMPLETED tasks")

	// The same filter board-wide: also none.
	failedBoardWide := callListTasks(t, ctx, session, map[string]any{"state": "FAILED"})
	assert.Empty(t, failedBoardWide, "state=FAILED board-wide must return no tasks")

	// state=COMPLETED with project_id: both tasks, proving the filter is
	// applied rather than the argument being dropped.
	completedInProject := callListTasks(t, ctx, session, map[string]any{"project_id": project.ID, "state": "COMPLETED"})
	assert.Len(t, completedInProject, 2, "state=COMPLETED must return the project's COMPLETED tasks")
}

func TestServer_GetTask_NotFound(t *testing.T) {
	s, _ := newTestServer(t)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "board.get_task",
		Arguments: map[string]any{"task_id": "nonexistent"},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "expected error for nonexistent task")
}

func TestServer_AddComment_EmptyBody(t *testing.T) {
	s, store := newTestServer(t)

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "test-project",
		Tasks: []models.DraftTask{
			{Title: "task-1", AgentID: "default"},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, tasks)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "board.add_comment",
		Arguments: map[string]any{"task_id": tasks[0].ID, "body": "  "},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "expected error for empty body")
}

func TestServer_UpdateTaskState_InvalidTransition(t *testing.T) {
	s, store := newTestServer(t)

	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "test-project",
		Tasks: []models.DraftTask{
			{Title: "task-1", AgentID: "default"},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, tasks)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	// PENDING -> COMPLETED is invalid.
	res, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "board.update_task_state",
		Arguments: map[string]any{"task_id": tasks[0].ID, "state": "COMPLETED"},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "expected error for invalid state transition")
}

func TestServer_ListProjects(t *testing.T) {
	s, store := newTestServer(t)

	project, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "my-project",
		Tasks:       []models.DraftTask{{Title: "t1", AgentID: "default"}},
	})
	require.NoError(t, err)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = s.mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "board.list_projects",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var projects []struct {
		ID            string `json:"id"`
		WorkspacePath string `json:"workspace_path"`
	}
	require.NoError(t, json.Unmarshal([]byte(text.Text), &projects))
	require.Len(t, projects, 1)
	require.Equal(t, project.ID, projects[0].ID)
	require.Equal(t, project.WorkspacePath, projects[0].WorkspacePath)
}
