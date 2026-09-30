package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agentd/internal/models"
)

func (s *Server) registerReadTools() {
	s.registerListTasks()
	s.registerGetTask()
	s.registerListProjects()
	s.registerGetProject()
	s.registerListComments()
}

func (s *Server) registerListTasks() {
	type input struct {
		ProjectID string `json:"project_id,omitempty"`
		State     string `json:"state,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Offset    int    `json:"offset,omitempty"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "board.list_tasks", Description: "List tasks on the kanban board, optionally filtered by project and state. Paginated: limit (default 200, the store's max page) and offset page through the whole board, so a >100-task export is never silently truncated."}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_tasks", "project_id", in.ProjectID, "state", in.State, "limit", in.Limit, "offset", in.Offset)
		if s.board == nil {
			return errorResult(fmt.Errorf("list_tasks requires KanbanBoardContract")), nil, nil
		}
		// Both the project-scoped and board-wide calls go through the same
		// paginated, filter-aware store method. The project_id shortcut this
		// replaced skipped the state filter (B-006) and the board-wide path
		// hard-coded a silent 100-task cap (B-005); routing both through
		// ListTasks makes the two consistent and the cap explicit/pageable.
		filter := models.TaskFilter{}
		if in.ProjectID != "" {
			filter.ProjectID = &in.ProjectID
		}
		if in.State != "" {
			filter.States = []models.TaskState{models.TaskState(in.State)}
		}
		if in.Limit <= 0 {
			in.Limit = 200
		}
		filter.Pagination = models.PaginationParams{Limit: in.Limit, Offset: in.Offset}
		result, err := s.board.ListTasks(ctx, filter)
		if err != nil {
			return errorResult(err), nil, nil
		}
		tasks := result.Data
		out := make([]map[string]any, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, map[string]any{"id": t.ID, "title": t.Title, "state": string(t.State), "assignee": string(t.Assignee), "project_id": t.ProjectID, "agent_id": t.AgentID, "depends_on": t.DependsOn})
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerGetTask() {
	type input struct {
		TaskID string `json:"task_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "board.get_task", Description: "Get a single task with full details including recent events."}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.get_task", "task_id", in.TaskID)
		task, err := s.store.GetTask(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		events, _ := s.store.ListEventsByTask(ctx, in.TaskID)
		comments, _ := s.store.ListComments(ctx, in.TaskID)
		out := map[string]any{"id": task.ID, "title": task.Title, "description": task.Description, "state": string(task.State), "assignee": string(task.Assignee), "project_id": task.ProjectID, "agent_id": task.AgentID, "depends_on": task.DependsOn, "retry_count": task.RetryCount, "events": len(events), "comments": len(comments)}
		return textResult(out), out, nil
	})
}

func (s *Server) registerListProjects() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "board.list_projects", Description: "List all projects on the board."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_projects")
		projects, err := s.store.ListProjects(ctx)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := make([]map[string]any, 0, len(projects))
		for _, p := range projects {
			out = append(out, map[string]any{"id": p.ID, "name": p.Name, "workspace_path": p.WorkspacePath, "status": string(p.Status)})
		}
		return textResult(out), out, nil
	})
}

func (s *Server) registerGetProject() {
	type input struct {
		ProjectID string `json:"project_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "board.get_project", Description: "Get a single project with its details."}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.get_project", "project_id", in.ProjectID)
		project, err := s.store.GetProject(ctx, in.ProjectID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := map[string]any{"id": project.ID, "name": project.Name, "original_input": project.OriginalInput, "workspace_path": project.WorkspacePath, "status": string(project.Status)}
		return textResult(out), out, nil
	})
}

func (s *Server) registerListComments() {
	type input struct {
		TaskID string `json:"task_id"`
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "board.list_comments", Description: "List comments on a task."}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		slog.DebugContext(ctx, "mcp: board.list_comments", "task_id", in.TaskID)
		comments, err := s.store.ListComments(ctx, in.TaskID)
		if err != nil {
			return errorResult(err), nil, nil
		}
		out := make([]map[string]any, 0, len(comments))
		for _, c := range comments {
			out = append(out, map[string]any{"id": c.ID, "author": string(c.Author), "body": c.Body})
		}
		return textResult(out), out, nil
	})
}
