// Package fakeagent is a scripted ACP agent for tests, built on the SDK's
// agent side, so no test needs a real agent or hand-written JSON-RPC. The
// script travels in the prompt, the way devenv/mockllm reads its tool calls
// from the request, so tests sharing one process never share state: a prompt
// that is a JSON list of steps runs them in order, and any other prompt gets
// one "ok" message.
//
// A test package runs it from TestMain, as its own test binary:
//
//	if len(os.Args) > 1 && os.Args[1] == fakeagent.Arg {
//		_ = fakeagent.Serve(os.Args[2:], os.Stdin, os.Stdout)
//		os.Exit(0)
//	}
package fakeagent

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// maxSleepMS caps a sleep step (one hour), so the millisecond conversion cannot overflow.
const maxSleepMS = 3_600_000

// Arg is the first argument that makes a test binary serve the fake agent.
const Arg = "fake-acp-agent"

// AuthMethod is the only method id Authenticate accepts.
const AuthMethod = "token"

// Serve runs the agent on in and out until the client disconnects. With
// "-protocol N" it answers initialize with protocol version N, and with
// "-model M" session/new reports M as its "model" config option.
func Serve(args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet(Arg, flag.ContinueOnError)
	protocol := flags.Int("protocol", acp.ProtocolVersionNumber, "protocol version initialize answers with")
	model := flags.String("model", "", "the model session/new reports")
	if err := flags.Parse(args); err != nil {
		return err
	}
	a := &agent{protocol: *protocol, model: *model, mcp: map[acp.SessionId]string{}, next: map[acp.SessionId][]Step{}}
	a.conn = acp.NewAgentSideConnection(a, out, in)
	<-a.conn.Done()
	return nil
}

type agent struct {
	conn     *acp.AgentSideConnection
	protocol int
	model    string
	sessions atomic.Int64
	cancels  atomic.Int64
	calls    atomic.Int64

	mu sync.Mutex
	// mcp is each session's agentd MCP url, next its steps for the next prompt.
	mcp  map[acp.SessionId]string
	next map[acp.SessionId][]Step
	// cwd is each session's workspace, where a Touch step writes.
	cwd map[acp.SessionId]string
}

var _ acp.Agent = (*agent)(nil)

func (a *agent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	methods := []acp.AuthMethod{{Agent: &acp.AuthMethodAgent{Id: AuthMethod, Name: "Token"}}}
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(a.protocol), AuthMethods: methods}, nil
}

func (a *agent) Authenticate(_ context.Context, p acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	if p.MethodId != AuthMethod {
		return acp.AuthenticateResponse{}, fmt.Errorf("unknown auth method %q", p.MethodId)
	}
	return acp.AuthenticateResponse{}, nil
}

// NewSession names the session after the process, so a test can tell a warm
// process from a restarted one.
func (a *agent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := acp.SessionId(fmt.Sprintf("sess-%d-%d", os.Getpid(), a.sessions.Add(1)))
	a.mu.Lock()
	if a.cwd == nil {
		a.cwd = map[acp.SessionId]string{}
	}
	a.cwd[id] = p.Cwd
	a.mu.Unlock()
	for _, server := range p.McpServers {
		if server.Http != nil && server.Http.Name == "agentd" {
			a.mu.Lock()
			a.mcp[id] = server.Http.Url
			a.mu.Unlock()
		}
	}
	return acp.NewSessionResponse{SessionId: id, ConfigOptions: modelOptions(a.model)}, nil
}

// Prompt runs the scripted steps, or the next scripted prompt for the session.
func (a *agent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	var steps []Step
	var text string
	if len(p.Prompt) == 1 && p.Prompt[0].Text != nil {
		text = p.Prompt[0].Text.Text
		steps = scriptFromPrompt(text)
	}
	if steps == nil {
		a.mu.Lock()
		steps = a.next[p.SessionId]
		delete(a.next, p.SessionId)
		a.mu.Unlock()
	}
	if steps == nil {
		steps = []Step{{Message: "ok"}}
	}
	var usage *acp.Usage
	for _, step := range steps {
		if step.Echo != "" {
			step = echoStep(step.Echo, text)
		}
		if step.Usage != nil {
			usage = step.Usage
		}
		if stop, err := a.run(ctx, p.SessionId, step); stop != "" || err != nil {
			return acp.PromptResponse{StopReason: stop, Usage: usage}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn, Usage: usage}, nil
}

// sideEffects does a step's stderr line and marker file, which a step can carry
// beside its main action.
func (a *agent) sideEffects(session acp.SessionId, step Step) error {
	if step.Stderr != "" {
		fmt.Fprintln(os.Stderr, step.Stderr)
	}
	if step.Touch == "" {
		return nil
	}
	a.mu.Lock()
	dir := a.cwd[session]
	a.mu.Unlock()
	return os.WriteFile(filepath.Join(dir, filepath.Base(step.Touch)), nil, 0o644)
}

// run does one step. A non-empty stop reason or an error ends the turn.
func (a *agent) run(ctx context.Context, session acp.SessionId, step Step) (acp.StopReason, error) {
	if err := a.sideEffects(session, step); err != nil {
		return "", err
	}
	switch {
	case step.Exit != 0:
		os.Exit(step.Exit)
	case step.Stop != "":
		return step.Stop, nil
	case step.Error != "":
		return "", errors.New(step.Error)
	case step.SleepMS > 0:
		select {
		case <-time.After(time.Duration(min(step.SleepMS, maxSleepMS)) * time.Millisecond):
		case <-ctx.Done():
			a.cancels.Add(1)
			return acp.StopReasonCancelled, nil
		}
		return "", nil
	case step.WaitCancel:
		<-ctx.Done()
		a.cancels.Add(1)
		return acp.StopReasonCancelled, nil
	case step.Cancels:
		return "", a.say(ctx, session, fmt.Sprintf("cancels: %d", a.cancels.Load()))
	case step.Permission != nil:
		return a.ask(ctx, session, *step.Permission)
	case step.Tool != nil:
		return "", a.callTool(ctx, session, *step.Tool)
	case step.Did != "":
		return "", a.did(ctx, session, step.Did)
	case step.Next != nil:
		a.mu.Lock()
		a.next[session] = step.Next
		a.mu.Unlock()
		return "", nil
	}
	if step.Message == "" {
		return "", nil
	}
	return "", a.say(ctx, session, step.Message)
}

func (a *agent) say(ctx context.Context, session acp.SessionId, text string) error {
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: session, Update: acp.UpdateAgentMessageText(text)})
}

// ask offers cline's options, but with allow_always first, so a client that
// takes the first option grants every later call too.
func (a *agent) ask(ctx context.Context, session acp.SessionId, p Permission) (acp.StopReason, error) {
	id := acp.ToolCallId(fmt.Sprintf("call-%d", a.calls.Add(1)))
	if p.CallID != "" {
		// Name the call in its start, then ask permission for it with the title
		// only: the driver must merge the recorded kind (ToolCallUpdate.Kind is
		// optional, so the request carries none).
		id = acp.ToolCallId(p.CallID)
		start := acp.StartToolCall(id, p.Title, acp.WithStartKind(p.Kind), acp.WithStartStatus(acp.ToolCallStatusPending))
		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: session, Update: start}); err != nil {
			return "", err
		}
		// shortcut: the SDK handles a notification and the next request concurrently, so wait for the start to be recorded; a real agent can race the same way.
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			a.cancels.Add(1)
			return acp.StopReasonCancelled, nil
		}
	}
	// A permission request carries the kind only when the agent names it; the
	// driver merges the recorded kind otherwise (ToolCallUpdate.Kind is
	// optional).
	toolCall := acp.ToolCallUpdate{ToolCallId: id, Title: acp.Ptr(p.Title), RawInput: p.RawInput}
	if p.Kind != "" && p.CallID == "" {
		toolCall.Kind = acp.Ptr(p.Kind)
	}
	resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: session,
		ToolCall:  toolCall,
		Options: []acp.PermissionOption{
			{OptionId: "allow_always", Name: "Allow always", Kind: acp.PermissionOptionKindAllowAlways},
			{OptionId: "allow_once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject_once", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return "", err
	}
	answer := "cancelled"
	if resp.Outcome.Selected != nil {
		answer = string(resp.Outcome.Selected.OptionId)
	}
	return "", a.say(ctx, session, "permission: "+answer)
}

// did reports a tool call as agents do: started pending, then completed.
func (a *agent) did(ctx context.Context, session acp.SessionId, kind acp.ToolKind) error {
	id := acp.ToolCallId(fmt.Sprintf("call-%d", a.calls.Add(1)))
	start := acp.StartToolCall(id, "did "+string(kind), acp.WithStartKind(kind), acp.WithStartStatus(acp.ToolCallStatusPending))
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: session, Update: start}); err != nil {
		return err
	}
	done := acp.UpdateToolCall(id, acp.WithUpdateStatus(acp.ToolCallStatusCompleted))
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: session, Update: done})
}

func (a *agent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func (a *agent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

func (a *agent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}

func (a *agent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}

func (a *agent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}

func (a *agent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (a *agent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}
