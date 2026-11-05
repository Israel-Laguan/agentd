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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Arg is the first argument that makes a test binary serve the fake agent.
const Arg = "fake-acp-agent"

// AuthMethod is the only method id Authenticate accepts.
const AuthMethod = "token"

// Step is one scripted action of a prompt turn. Set one field per step.
type Step struct {
	// Message streams one agent_message_chunk.
	Message string `json:"message,omitempty"`
	// Permission asks the client for permission and then streams
	// "permission: <optionId>" or "permission: cancelled".
	Permission *Permission `json:"permission,omitempty"`
	// WaitCancel blocks until the client cancels the prompt.
	WaitCancel bool `json:"wait_cancel,omitempty"`
	// Cancels streams "cancels: N", the prompts this process saw cancelled.
	Cancels bool `json:"cancels,omitempty"`
	// Stop ends the turn with this stop reason, the way opencode ends one
	// cancelled after a rejected permission.
	Stop acp.StopReason `json:"stop,omitempty"`
	// Error fails the prompt with this message.
	Error string `json:"error,omitempty"`
	// Exit, when not zero, ends the process with this code, mid-prompt.
	Exit int `json:"exit,omitempty"`
	// Tool calls a tool on the session's "agentd" http MCP server and
	// streams "tool: <text>" or "tool error: <text>".
	Tool *Tool `json:"tool,omitempty"`
	// Did reports a tool call of this kind that ran and completed.
	Did acp.ToolKind `json:"did,omitempty"`
	// Usage reports this turn's token counts, as an agent's PromptResponse does.
	Usage *acp.Usage `json:"usage,omitempty"`
	// Next is what the session's next prompt that is not a script runs, as
	// an agent answers agentd's own follow-up.
	Next []Step `json:"next,omitempty"`
}

// Tool is the MCP tool call a Tool step makes.
type Tool struct {
	Name string `json:"name"`
	Args any    `json:"args,omitempty"`
}

// Permission is the tool call a Permission step asks about.
type Permission struct {
	Kind     acp.ToolKind `json:"kind"`
	Title    string       `json:"title,omitempty"`
	RawInput any          `json:"raw_input,omitempty"`
}

// Script is the prompt that runs steps.
func Script(steps ...Step) string {
	b, err := json.Marshal(steps)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Serve runs the agent on in and out until the client disconnects. With
// "-protocol N" it answers initialize with protocol version N.
func Serve(args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet(Arg, flag.ContinueOnError)
	protocol := flags.Int("protocol", acp.ProtocolVersionNumber, "protocol version initialize answers with")
	if err := flags.Parse(args); err != nil {
		return err
	}
	a := &agent{protocol: *protocol, mcp: map[acp.SessionId]string{}, next: map[acp.SessionId][]Step{}}
	a.conn = acp.NewAgentSideConnection(a, out, in)
	<-a.conn.Done()
	return nil
}

type agent struct {
	conn     *acp.AgentSideConnection
	protocol int
	sessions atomic.Int64
	cancels  atomic.Int64
	calls    atomic.Int64

	mu sync.Mutex
	// mcp is each session's agentd MCP url, next its steps for the next prompt.
	mcp  map[acp.SessionId]string
	next map[acp.SessionId][]Step
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
	for _, server := range p.McpServers {
		if server.Http != nil && server.Http.Name == "agentd" {
			a.mu.Lock()
			a.mcp[id] = server.Http.Url
			a.mu.Unlock()
		}
	}
	return acp.NewSessionResponse{SessionId: id}, nil
}

// Prompt runs the scripted steps, or the next scripted prompt for the session.
func (a *agent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	var steps []Step
	if len(p.Prompt) == 1 && p.Prompt[0].Text != nil {
		steps = scriptFromPrompt(p.Prompt[0].Text.Text)
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
		if step.Usage != nil {
			usage = step.Usage
		}
		if stop, err := a.run(ctx, p.SessionId, step); stop != "" || err != nil {
			return acp.PromptResponse{StopReason: stop, Usage: usage}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn, Usage: usage}, nil
}

// run does one step. A non-empty stop reason or an error ends the turn.
func (a *agent) run(ctx context.Context, session acp.SessionId, step Step) (acp.StopReason, error) {
	switch {
	case step.Exit != 0:
		os.Exit(step.Exit)
	case step.Stop != "":
		return step.Stop, nil
	case step.Error != "":
		return "", errors.New(step.Error)
	case step.WaitCancel:
		<-ctx.Done()
		a.cancels.Add(1)
		return acp.StopReasonCancelled, nil
	case step.Cancels:
		return "", a.say(ctx, session, fmt.Sprintf("cancels: %d", a.cancels.Load()))
	case step.Permission != nil:
		return "", a.ask(ctx, session, *step.Permission)
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
func (a *agent) ask(ctx context.Context, session acp.SessionId, p Permission) error {
	resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: session,
		ToolCall:  acp.ToolCallUpdate{ToolCallId: acp.ToolCallId(fmt.Sprintf("call-%d", a.calls.Add(1))), Kind: acp.Ptr(p.Kind), Title: acp.Ptr(p.Title), RawInput: p.RawInput},
		Options: []acp.PermissionOption{
			{OptionId: "allow_always", Name: "Allow always", Kind: acp.PermissionOptionKindAllowAlways},
			{OptionId: "allow_once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject_once", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})
	if err != nil {
		return err
	}
	answer := "cancelled"
	if resp.Outcome.Selected != nil {
		answer = string(resp.Outcome.Selected.OptionId)
	}
	return a.say(ctx, session, "permission: "+answer)
}

func (a *agent) callTool(ctx context.Context, session acp.SessionId, t Tool) error {
	a.mu.Lock()
	url := a.mcp[session]
	a.mu.Unlock()
	client := mcp.NewClient(&mcp.Implementation{Name: "fakeagent", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		return a.say(ctx, session, "tool error: "+err.Error())
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: t.Name, Arguments: t.Args})
	if err != nil {
		return a.say(ctx, session, "tool error: "+err.Error())
	}
	var text []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = append(text, tc.Text)
		}
	}
	prefix := "tool: "
	if res.IsError {
		prefix = "tool error: "
	}
	return a.say(ctx, session, prefix+strings.Join(text, "\n"))
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
