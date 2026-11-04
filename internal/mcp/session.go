package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
	"agentd/internal/stage"
)

// ErrNoOutcome is returned when a session ended with neither report_outcome
// nor ask_human.
var ErrNoOutcome = errors.New("mcp: the agent reported no outcome")

// ErrReportedFailure is returned when the agent reported that it failed.
var ErrReportedFailure = errors.New("mcp: the agent reported failure")

// SessionConfig scopes one ACP session's server to the task it runs.
type SessionConfig struct {
	// ProjectID is the task's project, the only one the board reads see.
	ProjectID string
	// Contract is the stage's: a completed report_outcome must satisfy it.
	Contract stage.Contract
	// Context is the context pack agentd built, which get_context returns.
	Context string
}

// Session is one ACP session's MCP server (T-042): the contract tools and the
// board reads of one project, with no board write. It records what the agent
// reported, which the caller still checks against the workspace.
type Session struct {
	*Server
	cfg SessionConfig

	mu      sync.Mutex
	result  *stage.StageResult
	failed  bool
	summary string
	asked   bool
}

// NewSession returns the server for one session.
func NewSession(store models.KanbanStore, cfg SessionConfig) *Session {
	if cfg.ProjectID == "" {
		panic("mcp: NewSession requires a non-empty ProjectID (fail-closed board scope)")
	}
	s := &Session{Server: newServer(store, cfg.ProjectID), cfg: cfg}
	s.registerReadTools()
	s.registerReportOutcome()
	s.registerGetContext()
	s.registerAskHuman()
	return s
}

// Result is the stage result the agent reported: ErrNoOutcome when it reported
// none, ErrReportedFailure when it reported that it failed.
func (s *Session) Result() (stage.StageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.result != nil:
		return *s.result, nil
	case s.failed:
		return stage.StageResult{}, fmt.Errorf("%w: %s", ErrReportedFailure, s.summary)
	}
	return stage.StageResult{}, ErrNoOutcome
}

func (s *Session) registerReportOutcome() {
	type input struct {
		Outcome string `json:"outcome" jsonschema:"completed or failed"`
		Summary string `json:"summary" jsonschema:"what was done, or why it could not be done"`
		Result  any    `json:"result,omitempty" jsonschema:"the result the task asks for, required when completed"`
	}
	description := "Report the outcome of the task, once, when you are done. Use ask_human instead when a detail is missing."
	if hint := s.cfg.Contract.Hint; hint != "" {
		description += " A completed result must be: " + hint
	}
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "report_outcome", Description: description}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		// A guess after a question is not an answer to it (SP-025: cline did that).
		if s.asked {
			return errorResult(errors.New("a question is already with a human; end your turn")), nil, nil
		}
		switch {
		case in.Outcome == "failed":
			s.result, s.failed, s.summary = nil, true, in.Summary
			return textResult(map[string]any{"status": "recorded"}), nil, nil
		case in.Outcome != "completed":
			return s.reportError(fmt.Errorf("outcome must be completed or failed, not %q", in.Outcome)), nil, nil
		case in.Result == nil:
			return s.reportError(errors.New("result is required when completed")), nil, nil
		}
		raw, err := json.Marshal(in.Result)
		if err == nil {
			raw, err = s.cfg.Contract.Check(raw)
		}
		if err != nil {
			return s.reportError(err), nil, nil
		}
		s.result, s.failed = &stage.StageResult{Outcome: stage.OutcomeCompleted, Output: raw}, false
		return textResult(map[string]any{"status": "recorded"}), nil, nil
	})
}

// reportError tells the agent why its report was refused, with the shape a
// result must have, so it can report again.
func (s *Session) reportError(err error) *mcp.CallToolResult {
	message := err.Error() + ". Call report_outcome again"
	if hint := s.cfg.Contract.Hint; hint != "" {
		message += "; a completed result must be: " + hint
	}
	return errorResult(errors.New(message))
}

func (s *Session) registerGetContext() {
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: "get_context", Description: "Get the task's context: the instructions and facts agentd gathered for it."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s.cfg.Context}}}, nil, nil
	})
}

// registerAskHuman offers the builtin ask_human. A question is the stage
// seam's needs-human reply ({"needs_human": true, "question", "options"}),
// which the caller hands to a person as it does a model's.
func (s *Session) registerAskHuman() {
	description := agenttools.AskHumanToolDefinition().Description
	mcp.AddTool(s.mcpServer, &mcp.Tool{Name: agenttools.ToolNameAskHuman, Description: description}, func(_ context.Context, _ *mcp.CallToolRequest, in agenttools.AskHumanArgs) (*mcp.CallToolResult, any, error) {
		raw, _ := json.Marshal(in)
		args, err := agenttools.ParseAskHumanArgs(string(raw))
		if err != nil {
			return errorResult(fmt.Errorf("invalid ask_human arguments: %w", err)), nil, nil
		}
		output, err := json.Marshal(map[string]any{"needs_human": true, "question": args.Question, "options": args.Options})
		if err != nil {
			return errorResult(err), nil, nil
		}
		s.mu.Lock()
		if s.asked {
			s.mu.Unlock()
			return errorResult(errors.New("a question is already with a human; end your turn")), nil, nil
		}
		s.result, s.failed, s.asked = &stage.StageResult{Outcome: stage.OutcomeNeedsHuman, Output: output}, false, true
		s.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "The question was sent to a human. End your turn now; the task waits for the answer."}}}, nil, nil
	})
}

// SessionMux serves each session's server at its own unguessable path, so one
// listener (the run's MCP socket) serves every session of a warm agent, and a
// session never reaches another's tools.
type SessionMux struct {
	mu       sync.Mutex
	sessions map[string]http.Handler
}

// Add serves h at the path it returns, until remove is called.
func (m *SessionMux) Add(h http.Handler) (path string, remove func()) {
	id := make([]byte, 16)
	_, _ = rand.Read(id) // crypto/rand.Read never fails
	path = "/mcp/" + hex.EncodeToString(id)
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions = map[string]http.Handler{}
	}
	m.sessions[path] = h
	m.mu.Unlock()
	return path, func() {
		m.mu.Lock()
		delete(m.sessions, path)
		m.mu.Unlock()
	}
}

func (m *SessionMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	h := m.sessions[strings.TrimSuffix(r.URL.Path, "/")]
	m.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h.ServeHTTP(w, r)
}
