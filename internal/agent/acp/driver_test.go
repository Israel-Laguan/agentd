package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"

	acpsdk "github.com/coder/acp-go-sdk"
)

// TestMain doubles as the fake agent: `<test binary> fake-acp-agent ...`
// serves it on stdio, so the driver spawns a real process.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == fakeagent.Arg {
		if err := fakeagent.Serve(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeDriver(t *testing.T, cfg Config, args ...string) *Driver {
	t.Helper()
	cfg.Command = func(ctx context.Context, _ string) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, os.Args[0], append([]string{fakeagent.Arg}, args...)...), nil
	}
	if cfg.Cwd == "" {
		cfg.Cwd = t.TempDir()
	}
	d := NewDriver(cfg)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func call(t *testing.T, d *Driver, req Request, steps ...fakeagent.Step) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req.Prompt = fakeagent.Script(steps...)
	return d.Call(ctx, req)
}

// The process starts for the driver's workspace, the one every session opens
// in, so the jail binds the workspace the agent works in.
func TestTheAgentStartsForTheDriversWorkspace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var started string
	d := NewDriver(Config{Cwd: dir, Command: func(ctx context.Context, cwd string) (*exec.Cmd, error) {
		started = cwd
		return exec.CommandContext(ctx, os.Args[0], fakeagent.Arg), nil
	}})
	t.Cleanup(func() { _ = d.Close() })
	if _, err := call(t, d, Request{}); err != nil || started != dir {
		t.Fatalf("err = %v, started for %q; want %q", err, started, dir)
	}
}

// An agent that streams without end cannot grow the call's text past maxText.
func TestStreamedTextIsCapped(t *testing.T) {
	t.Parallel()
	chunk := strings.Repeat("a", maxText/2+1)
	r, err := call(t, fakeDriver(t, Config{}), Request{}, fakeagent.Step{Message: chunk}, fakeagent.Step{Message: chunk})
	if err != nil || len(r.Text) != maxText {
		t.Fatalf("err = %v, text is %d bytes; want %d", err, len(r.Text), maxText)
	}
	// The cap falls inside a two-byte rune, which is dropped, not cut.
	r, err = call(t, fakeDriver(t, Config{}), Request{}, fakeagent.Step{Message: "a" + strings.Repeat("é", maxText/2)})
	if err != nil || !utf8.ValidString(r.Text) || len(r.Text) != maxText-1 {
		t.Fatalf("err = %v, text is %d bytes, valid UTF-8 %v; want %d valid bytes", err, len(r.Text), utf8.ValidString(r.Text), maxText-1)
	}
}

// pid is the agent process a fake session ran in ("sess-<pid>-<n>").
func pid(t *testing.T, r Result) string {
	t.Helper()
	parts := strings.Split(r.SessionID, "-")
	if len(parts) != 3 {
		t.Fatalf("session id %q is not the fake's", r.SessionID)
	}
	return parts[1]
}

func TestCallEndsWithTheFinalMessage(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	r, err := call(t, d, Request{}, fakeagent.Step{Message: "hello "}, fakeagent.Step{Message: "world"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if r.Text != "hello world" || r.StopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("text = %q, stop = %q; want the streamed message and end_turn", r.Text, r.StopReason)
	}
}

func TestWarmProcessServesOneSessionPerCall(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	first, err := call(t, d, Request{}, fakeagent.Step{Message: "one"})
	if err != nil {
		t.Fatalf("first Call: %v", err)
	}
	second, err := call(t, d, Request{}, fakeagent.Step{Message: "two"})
	if err != nil {
		t.Fatalf("second Call: %v", err)
	}
	if first.SessionID == second.SessionID || pid(t, first) != pid(t, second) {
		t.Fatalf("sessions %q and %q; want two sessions in one process", first.SessionID, second.SessionID)
	}
}

func TestAgentDeathFailsTheCallAndTheNextCallStartsANewProcess(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	before, err := call(t, d, Request{}, fakeagent.Step{Message: "up"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	_, err = call(t, d, Request{}, fakeagent.Step{Exit: 3})
	if err == nil || !strings.Contains(err.Error(), "agent process exited") {
		t.Fatalf("Call on a dying agent: err = %v; want the exit named", err)
	}
	after, err := call(t, d, Request{}, fakeagent.Step{Message: "back"})
	if err != nil {
		t.Fatalf("Call after the death: %v", err)
	}
	if after.Text != "back" || pid(t, after) == pid(t, before) {
		t.Fatalf("after the death: text = %q, session %q (before %q); want a new process", after.Text, after.SessionID, before.SessionID)
	}
}

func TestProtocolVersionMismatchFailsAtInitialize(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{}, "-protocol", "2")
	_, err := call(t, d, Request{}, fakeagent.Step{Message: "never"})
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("err = %v; want ErrProtocolVersion", err)
	}
	if !strings.Contains(err.Error(), "initialize") || !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "pins 1") {
		t.Fatalf("err = %q; want initialize, the agent's version and the pinned one named", err)
	}
}

func TestCancelMidPromptReachesTheAgentAndKeepsTheProcess(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel once the agent is working, so the cancel lands mid-prompt.
	req := Request{OnUpdate: func(u acpsdk.SessionUpdate) {
		if u.AgentMessageChunk != nil {
			cancel()
		}
	}}
	req.Prompt = fakeagent.Script(fakeagent.Step{Message: "working"}, fakeagent.Step{WaitCancel: true})
	before, err := d.Call(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Call: err = %v; want context.Canceled", err)
	}
	var after Result
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if after, err = call(t, d, Request{}, fakeagent.Step{Cancels: true}); err != nil || after.Text == "cancels: 1" {
			break
		}
	}
	if err != nil || after.Text != "cancels: 1" || pid(t, after) != pid(t, before) {
		t.Fatalf("after cancel: err = %v, text = %q, session %q (before %q); want the agent to have seen one cancel in the same process",
			err, after.Text, after.SessionID, before.SessionID)
	}
}

// An agent's quota or rate-limit failure is the gateway's: errors.Is
// ErrLLMQuotaExceeded sends it down the breaker and defer path.
func TestQuotaErrorsMapToTheBreakerPath(t *testing.T) {
	t.Parallel()
	d := fakeDriver(t, Config{})
	for _, msg := range []string{"429 Too Many Requests", "Rate limit reached for model", "Insufficient balance"} {
		_, err := call(t, d, Request{}, fakeagent.Step{Error: msg})
		if !errors.Is(err, models.ErrLLMQuotaExceeded) {
			t.Errorf("agent error %q: err = %v; want ErrLLMQuotaExceeded", msg, err)
		}
	}
	_, err := call(t, d, Request{}, fakeagent.Step{Error: "tool crashed"})
	if err == nil || errors.Is(err, models.ErrLLMQuotaExceeded) {
		t.Fatalf("other agent error: err = %v; want a plain failure", err)
	}
}

func TestAuthenticateUsesTheConfiguredMethod(t *testing.T) {
	t.Parallel()
	if _, err := call(t, fakeDriver(t, Config{AuthMethod: fakeagent.AuthMethod}), Request{}, fakeagent.Step{Message: "in"}); err != nil {
		t.Fatalf("known method: %v", err)
	}
	_, err := call(t, fakeDriver(t, Config{AuthMethod: "browser"}), Request{}, fakeagent.Step{Message: "in"})
	if err == nil || !strings.Contains(err.Error(), "authenticate") {
		t.Fatalf("unknown method: err = %v; want authenticate to fail the call", err)
	}
}

// A read-only session refuses every write before the hooks, which would allow
// it; a command still reaches them, and the read-only jail confines it.
func TestReadOnlySessionRefusesWrites(t *testing.T) {
	t.Parallel()
	var asked []string
	permit := func(_ context.Context, p Permission) (Verdict, error) {
		asked = append(asked, p.Tool)
		return Verdict{Allow: true}, nil
	}
	r, err := call(t, fakeDriver(t, Config{}), Request{ReadOnly: true, Permit: permit},
		fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindEdit, RawInput: map[string]any{"path": "a.go"}}},
		fakeagent.Step{Message: " "},
		fakeagent.Step{Permission: &fakeagent.Permission{Kind: acpsdk.ToolKindExecute, RawInput: map[string]any{"command": "ls"}}})
	if err != nil || r.Text != "permission: reject_once permission: allow_once" || strings.Join(asked, ",") != "bash" {
		t.Fatalf("err = %v, text = %q, hooks asked for %v; want the edit refused before the hooks and ls allowed", err, r.Text, asked)
	}
}

// Evidence counts the tool calls that completed and changed the workspace or
// ran a command, the B-082 gate's passed commands and written files.
func TestCompletedWorkIsEvidence(t *testing.T) {
	t.Parallel()
	r, err := call(t, fakeDriver(t, Config{}), Request{},
		fakeagent.Step{Did: acpsdk.ToolKindEdit}, fakeagent.Step{Did: acpsdk.ToolKindExecute},
		fakeagent.Step{Did: acpsdk.ToolKindRead}, fakeagent.Step{Did: acpsdk.ToolKindSearch})
	if err != nil || r.Evidence != 2 {
		t.Fatalf("err = %v, evidence = %d; want the edit and the command", err, r.Evidence)
	}
}

// Followup prompts again in the same session until it has nothing to say.
func TestFollowupPromptsAgainInTheSameSession(t *testing.T) {
	t.Parallel()
	prompts := 0
	followup := func() string {
		prompts++
		if prompts > 1 {
			return ""
		}
		return "repair"
	}
	r, err := call(t, fakeDriver(t, Config{}), Request{Followup: followup},
		fakeagent.Step{Message: "one "}, fakeagent.Step{Next: []fakeagent.Step{{Message: "two"}}})
	if err != nil || r.Text != "one two" || prompts != 2 {
		t.Fatalf("err = %v, text = %q, followup called %d times; want two turns of one session", err, r.Text, prompts)
	}
}
