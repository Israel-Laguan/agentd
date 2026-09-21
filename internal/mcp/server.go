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
	s.registerReadTools()
	s.registerWriteTools()
	return s
}

// Run starts the server on the stdio transport. Blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	if s.mcpServer == nil {
		return fmt.Errorf("mcp server not initialized")
	}
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

func (s *Server) registerWriteTools() {
	s.registerAddComment()
	s.registerUpdateTaskState()
	s.registerAssignTask()
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
