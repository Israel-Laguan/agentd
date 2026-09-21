package mcp

import (
	"context"
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

	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
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
}
