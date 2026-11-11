package acp

import (
	"cmp"
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
	id     acpsdk.SessionId
	ctx    context.Context
	cancel context.CancelFunc
	req    Request

	mu      sync.Mutex
	text    strings.Builder
	suspend string
	// refused is the reason of the last permission agentd refused.
	refused string
	// calls are the tool calls, by their latest kind and status: ACP lets a
	// later update replace either, so evidence uses both as last reported.
	calls map[acpsdk.ToolCallId]toolCall
}

type toolCall struct {
	kind   acpsdk.ToolKind
	status acpsdk.ToolCallStatus
}

// evidenceKinds are the tool kinds whose completed call is evidence of work:
// a write, or a command, the builtin gate's file written and command passed.
// shortcut: a completed command is not known to have passed (ACP reports no exit code); diff the workspace (PR 08's seed check) if an agent misreports.
var evidenceKinds = map[acpsdk.ToolKind]bool{
	acpsdk.ToolKindExecute: true,
	acpsdk.ToolKindEdit:    true,
	acpsdk.ToolKindDelete:  true,
	acpsdk.ToolKindMove:    true,
}

func (c *client) open(ctx context.Context, id acpsdk.SessionId, req Request) *session {
	s := &session{id: id, req: req, calls: map[acpsdk.ToolCallId]toolCall{}}
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

func (s *session) evidence() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, call := range s.calls {
		if call.status == acpsdk.ToolCallStatusCompleted && evidenceKinds[call.kind] {
			n++
		}
	}
	return n
}

// noteToolCall records a tool call's latest kind and status. ACP lets either
// change in a later update, so a call counts as evidence only when both its
// latest values qualify.
// recordedKind is the kind ACP last recorded for a tool call, or "" when the
// call is unknown or its kind was never reported. A later request_permission
// may omit it, so the driver falls back to this before refusing the call.
func (s *session) recordedKind(id acpsdk.ToolCallId) acpsdk.ToolKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[id].kind
}

func (s *session) noteToolCall(id acpsdk.ToolCallId, kind *acpsdk.ToolKind, status *acpsdk.ToolCallStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.calls[id]
	if kind != nil && *kind != "" {
		call.kind = *kind
	}
	if status != nil {
		call.status = *status
	}
	s.calls[id] = call
}

// addUsage sums the turns' counts.
func addUsage(total, turn *acpsdk.Usage) *acpsdk.Usage {
	if total == nil || turn == nil {
		return cmp.Or(turn, total)
	}
	sum := *total
	sum.InputTokens += turn.InputTokens
	sum.OutputTokens += turn.OutputTokens
	sum.TotalTokens += turn.TotalTokens
	return &sum
}

func (c *client) SessionUpdate(_ context.Context, n acpsdk.SessionNotification) error {
	s := c.session(n.SessionId)
	if s == nil {
		return nil
	}
	if chunk := n.Update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
		s.mu.Lock()
		text := chunk.Content.Text.Text
		// The JSON-decoded chunk is valid UTF-8, so this drops only a rune the cap cut.
		text = strings.ToValidUTF8(text[:min(len(text), maxText-s.text.Len())], "")
		s.text.WriteString(text)
		s.mu.Unlock()
	}
	if call := n.Update.ToolCall; call != nil {
		s.noteToolCall(call.ToolCallId, &call.Kind, &call.Status)
	}
	if call := n.Update.ToolCallUpdate; call != nil {
		s.noteToolCall(call.ToolCallId, call.Kind, call.Status)
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
	perms := newPermissions(p.ToolCall)
	// A later request_permission may omit the kind ACP recorded for the call
	// (ToolCallUpdate.Kind is optional). Merge the recorded kind before the
	// check: preserve an explicit request kind, otherwise use the recorded one,
	// and keep the request denied when neither is known (decide refuses an
	// empty kind). This lets an agent that first named the call report_outcome
	// reach the driver instead of being refused as unknown.
	if perms[0].Kind == "" {
		if k := s.recordedKind(p.ToolCall.ToolCallId); k != "" {
			for i := range perms {
				perms[i].Kind = string(k)
				perms[i].Tool = toolForKind[k]
			}
		}
	}
	perm := perms[0]
	v := Verdict{Allow: true}
	if !ownTool(perm, s.req.MCPServers) {
		v = decideAll(s.ctx, s.req.Permit, perms, s.req.ReadOnly, s.req.NoWorkspace)
	}
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
