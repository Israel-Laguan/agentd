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
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"
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
	// Exit ends the process with this code, mid-prompt.
	Exit int `json:"exit,omitempty"`
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
	a := &agent{protocol: *protocol}
	a.conn = acp.NewAgentSideConnection(a, out, in)
	<-a.conn.Done()
	return nil
}

type agent struct {
	conn     *acp.AgentSideConnection
	protocol int
	sessions atomic.Int64
	cancels  atomic.Int64
}

var _ acp.Agent = (*agent)(nil)

func (a *agent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(a.protocol), AuthMethods: []acp.AuthMethod{}}, nil
}

func (a *agent) Authenticate(_ context.Context, p acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	if p.MethodId != AuthMethod {
		return acp.AuthenticateResponse{}, fmt.Errorf("unknown auth method %q", p.MethodId)
	}
	return acp.AuthenticateResponse{}, nil
}

// NewSession names the session after the process, so a test can tell a warm
// process from a restarted one.
func (a *agent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := fmt.Sprintf("sess-%d-%d", os.Getpid(), a.sessions.Add(1))
	return acp.NewSessionResponse{SessionId: acp.SessionId(id)}, nil
}

func (a *agent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	var steps []Step
	if len(p.Prompt) != 1 || p.Prompt[0].Text == nil || json.Unmarshal([]byte(p.Prompt[0].Text.Text), &steps) != nil {
		steps = []Step{{Message: "ok"}}
	}
	for _, step := range steps {
		if stop, err := a.run(ctx, p.SessionId, step); stop != "" || err != nil {
			return acp.PromptResponse{StopReason: stop}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
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
		ToolCall:  acp.ToolCallUpdate{ToolCallId: "call-1", Kind: acp.Ptr(p.Kind), Title: acp.Ptr(p.Title), RawInput: p.RawInput},
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
