// Package mcp implements the MCP board-export server that exposes agentd's
// kanban board over the Model Context Protocol. External agents (Claude Code,
// Cline) act as workers behind agentd's control plane.
//
// Two transports are supported:
//   - stdio: for Claude Code/Cline native integration
//   - Streamable HTTP: at /mcp for programmatic access
//
// agentd owns the board; external agents are workers. All write operations
// are routed through agentd's existing store methods, which enforce the same
// state machine and approval boundaries as the REST API.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agentd/internal/models"
)

// Server wraps the MCP server and exposes board tools.
type Server struct {
	mcpServer *mcp.Server
	store     models.KanbanStore
	board     models.KanbanBoardContract
}

// New creates a new MCP board-export server backed by the given store.
// The store must also implement KanbanBoardContract for list/filter operations.
func New(store models.KanbanStore) *Server {
	s := &Server{store: store}
	if bc, ok := store.(models.KanbanBoardContract); ok {
		s.board = bc
	}
	s.mcpServer = mcp.NewServer(
		&mcp.Implementation{
			Name:    "agentd-mcp",
			Version: "0.1.0",
		},
		nil,
	)
	s.registerTools()
	return s
}

// Run starts the server on the stdio transport. Blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	slog.Info("mcp server: starting stdio transport")
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// HTTPHandler returns an http.Handler for the Streamable HTTP transport.
func (s *Server) HTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcpServer
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

// registerTools registers all board tools on the MCP server.
func (s *Server) registerTools() {
	s.registerListTasks()
	s.registerGetTask()
	s.registerListProjects()
	s.registerGetProject()
	s.registerListComments()
	s.registerAddComment()
	s.registerUpdateTaskState()
	s.registerAssignTask()
}

// --- Read tools ---

func (s *Server) registerListTasks() {
	type listTasksInput struct {
		ProjectID string `json:"project_id,omitempty"`
		State     string `json:"state,omitempty"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.list_tasks",
		Description: "List tasks on the kanban board, optionally filtered by project or state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTasksInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_tasks", "project_id", in.ProjectID, "state", in.State)
		var tasks []models.Task
		if in.ProjectID != "" {
			// Prefer ListTasksByProject which is on KanbanStore directly.
			var err error
			tasks, err = s.store.ListTasksByProject(ctx, in.ProjectID)
			if err != nil {
				return errorResult(err), nil, nil
			}
		} else if s.board != nil {
			filter := models.TaskFilter{}
			if in.State != "" {
				state := models.TaskState(in.State)
				filter.States = []models.TaskState{state}
			}
			filter.Pagination.Limit = 100
			result, err := s.board.ListTasks(ctx, filter)
			if err != nil {
				return errorResult(err), nil, nil
			}
			tasks = result.Data
		} else {
			return errorResult(fmt.Errorf("list_tasks without project_id requires KanbanBoardContract")), nil, nil
		}
		out := make([]map[string]any, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, map[string]any{
				"id":         t.ID,
				"title":      t.Title,
				"state":      string(t.State),
				"assignee":   string(t.Assignee),
				"project_id": t.ProjectID,
				"agent_id":   t.AgentID,
				"depends_on": t.DependsOn,
			})
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerGetTask() {
	type getTaskInput struct {
		TaskID string `json:"task_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.get_task",
		Description: "Get a single task with full details including recent events.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTaskInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.get_task", "task_id", in.TaskID)
		task, err := s.store.GetTask(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		events, _ := s.store.ListEventsByTask(ctx, in.TaskID)
		comments, _ := s.store.ListComments(ctx, in.TaskID)
		out := map[string]any{
			"id":          task.ID,
			"title":       task.Title,
			"description": task.Description,
			"state":       string(task.State),
			"assignee":    string(task.Assignee),
			"project_id":  task.ProjectID,
			"agent_id":    task.AgentID,
			"depends_on":  task.DependsOn,
			"retry_count": task.RetryCount,
			"events":      len(events),
			"comments":    len(comments),
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerListProjects() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.list_projects",
		Description: "List all projects on the board.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_projects")
		projects, err := s.store.ListProjects(ctx)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := make([]map[string]any, 0, len(projects))
		for _, p := range projects {
			out = append(out, map[string]any{
				"id":     p.ID,
				"name":   p.Name,
				"status": string(p.Status),
			})
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerGetProject() {
	type getProjectInput struct {
		ProjectID string `json:"project_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.get_project",
		Description: "Get a single project with its details.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getProjectInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.get_project", "project_id", in.ProjectID)
		project, err := s.store.GetProject(ctx, in.ProjectID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := map[string]any{
			"id":             project.ID,
			"name":           project.Name,
			"original_input": project.OriginalInput,
			"workspace_path": project.WorkspacePath,
			"status":         string(project.Status),
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerListComments() {
	type listCommentsInput struct {
		TaskID string `json:"task_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.list_comments",
		Description: "List comments on a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCommentsInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_comments", "task_id", in.TaskID)
		comments, err := s.store.ListComments(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := make([]map[string]any, 0, len(comments))
		for _, c := range comments {
			out = append(out, map[string]any{
				"id":     c.ID,
				"author": string(c.Author),
				"body":   c.Body,
			})
		}
		return textResult(out), out, nil
	})
}

// --- Write tools (gated through store methods) ---

func (s *Server) registerAddComment() {
	type addCommentInput struct {
		TaskID string `json:"task_id"`
		Body   string `json:"body"`
		Author string `json:"author,omitempty"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.add_comment",
		Description: "Add a comment to a task. Writes are gated through agentd's store (approval boundary).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addCommentInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.add_comment",
			"task_id", in.TaskID, "author", in.Author)
		if strings.TrimSpace(in.Body) == "" {
			return errorResult(fmt.Errorf("comment body is required")), nil, nil
		}
		author := models.NormalizeCommentAuthor(in.Author)
		if author == "" {
			author = models.CommentAuthorWorkerAgent
		}
		_, err := s.store.GetTask(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		err = s.store.AddComment(ctx, models.Comment{
			TaskID: in.TaskID,
			Author: author,
			Body:   in.Body,
		})
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := map[string]any{"status": "ok", "task_id": in.TaskID}
		return textResult(out), out, nil
	})
}

func (s *Server) registerUpdateTaskState() {
	type updateStateInput struct {
		TaskID string `json:"task_id"`
		State  string `json:"state"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.update_task_state",
		Description: "Transition a task to a new state. Writes are routed through agentd's state machine (approval boundary).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateStateInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.update_task_state",
			"task_id", in.TaskID, "target_state", in.State)
		task, err := s.store.GetTask(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		next := models.TaskState(in.State)
		if !task.State.CanTransitionTo(next) {
			return errorResult(fmt.Errorf("invalid transition %s -> %s", task.State, next)), nil, nil
		}
		updated, err := s.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, next)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := map[string]any{"status": "ok", "task_id": updated.ID, "state": string(updated.State)}
		return textResult(out), out, nil
	})
}

func (s *Server) registerAssignTask() {
	type assignInput struct {
		TaskID  string `json:"task_id"`
		AgentID string `json:"agent_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "board.assign_task",
		Description: "Assign a task to an agent. Writes are gated through agentd's store.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in assignInput) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.assign_task",
			"task_id", in.TaskID, "agent_id", in.AgentID)
		task, err := s.store.GetTask(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		updated, err := s.store.AssignTaskAgent(ctx, task.ID, task.UpdatedAt, in.AgentID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := map[string]any{"status": "ok", "task_id": updated.ID, "assignee": string(updated.Assignee)}
		return textResult(out), out, nil
	})
}

// --- Helpers ---

func textResult(data any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		b = []byte(fmt.Sprintf("%v", data))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("error: %v", err)}},
		IsError: true,
	}
}
