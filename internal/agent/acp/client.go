package acp

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
)

// client is agentd's side of the connection to one agent process. Each Call
// registers its session, so that session's updates and permission requests
// reach that call.
type client struct {
	mu       sync.Mutex
	sessions map[acpsdk.SessionId]*session
}

var _ acpsdk.Client = (*client)(nil)

// maxText caps the streamed message a call keeps: the SDK bounds one line,
// not a turn, and agentd reads a call's outcome elsewhere (report_outcome).
const maxText = 1 << 20

// session is one Call's state. ctx is the call's, cancelled to stop the turn.
type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	req    Request

	mu      sync.Mutex
	text    strings.Builder
	suspend string
	// refused is the reason of the last permission agentd refused.
	refused string
}

func (c *client) open(ctx context.Context, id acpsdk.SessionId, req Request) *session {
	s := &session{req: req}
	s.ctx, s.cancel = context.WithCancel(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[id] = s
	return s
}

func (c *client) close(id acpsdk.SessionId) {
	c.mu.Lock()
	s := c.sessions[id]
	delete(c.sessions, id)
	c.mu.Unlock()
	if s != nil {
		s.cancel()
	}
}

func (c *client) session(id acpsdk.SessionId) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

func (s *session) message() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.String()
}

func (s *session) suspended() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suspend
}

func (s *session) refusal() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

func (c *client) SessionUpdate(_ context.Context, n acpsdk.SessionNotification) error {
	s := c.session(n.SessionId)
	if s == nil {
		return nil
	}
	if chunk := n.Update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
		s.mu.Lock()
		text := chunk.Content.Text.Text
		text = text[:min(len(text), maxText-s.text.Len())]
		s.text.WriteString(text)
		s.mu.Unlock()
	}
	if s.req.OnUpdate != nil {
		s.req.OnUpdate(n.Update)
	}
	return nil
}

// RequestPermission answers through decide. A suspending verdict also cancels
// the turn, so the agent stops until the human decides. A request for a call
// that is over is cancelled, as ACP asks of a cancelled turn.
func (c *client) RequestPermission(_ context.Context, p acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	s := c.session(p.SessionId)
	if s == nil {
		return acpsdk.RequestPermissionResponse{Outcome: acpsdk.RequestPermissionOutcome{Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{}}}, nil
	}
	perm := newPermission(p.ToolCall)
	v := decide(s.ctx, s.req.Permit, perm)
	if !v.Allow || v.Suspend {
		// No command: it may carry a secret, and this log line is not scrubbed.
		slog.Info("acp: permission refused", "session", p.SessionId, "kind", perm.Kind, "tool", perm.Tool,
			"suspend", v.Suspend, "reason", v.Reason)
		s.mu.Lock()
		s.refused = v.Reason
		if s.refused == "" {
			s.refused = "a permission check refused a call"
		}
		s.mu.Unlock()
	}
	if v.Suspend {
		s.mu.Lock()
		s.suspend = v.Reason
		if s.suspend == "" {
			s.suspend = "a permission check blocked the task"
		}
		s.mu.Unlock()
		s.cancel()
		v.Allow = false
	}
	return acpsdk.RequestPermissionResponse{Outcome: outcome(p.Options, v.Allow)}, nil
}

// The client advertises no fs or terminal capability: neither agent used them
// (SP-022, SP-025). Serving them through the sandbox is T-042 Done-when 2.

func (c *client) ReadTextFile(context.Context, acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodFsReadTextFile)
}

func (c *client) WriteTextFile(context.Context, acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodFsWriteTextFile)
}

func (c *client) CreateTerminal(context.Context, acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalCreate)
}

func (c *client) KillTerminal(context.Context, acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalKill)
}

func (c *client) TerminalOutput(context.Context, acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalOutput)
}

func (c *client) ReleaseTerminal(context.Context, acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalRelease)
}

func (c *client) WaitForTerminalExit(context.Context, acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, acpsdk.NewMethodNotFound(acpsdk.ClientMethodTerminalWaitForExit)
}
