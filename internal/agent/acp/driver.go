// Package acp drives an external agent over the Agent Client Protocol (T-042).
// It is one driver for every agent: a preset is data, its command and auth
// method. github.com/coder/acp-go-sdk owns the protocol; this package adds the
// process lifecycle, one session per call on a warm process, the permission
// policy and the error mapping. It holds no board types, so it can move to the
// agent kit (SP-024).
package acp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"agentd/internal/models"

	acpsdk "github.com/coder/acp-go-sdk"
)

// ErrProtocolVersion is returned when the agent answers initialize with a
// protocol version other than the one the SDK speaks.
var ErrProtocolVersion = errors.New("acp: protocol version mismatch")

// ErrSuspended is returned when a permission verdict blocked the task on a
// human: the call stopped, and a later call asks again once the human decided.
var ErrSuspended = errors.New("acp: call suspended for human approval")

// ErrPermissionRefused is returned when the agent ended its turn cancelled
// after agentd refused one of its permissions, as opencode does: the turn
// stopped short of its work, though the agent reported no error.
var ErrPermissionRefused = errors.New("acp: turn ended on a refused permission")

// quotaError matches the agent errors that mean its provider refused for quota
// or rate. An agent reports a provider's 429 only as text in a JSON-RPC error,
// and cline's empty balance as "Insufficient balance" (docs/acp-agents.md).
var quotaError = regexp.MustCompile(`(?i)\b429\b|rate[ _-]?limit|too many requests|quota|insufficient (?:balance|credits)`)

// Config is how to start one agent for one workspace.
type Config struct {
	// Cwd is the workspace: the process starts for it and every session opens
	// in it. A warm process is jailed to the workspace it started for, so a
	// Driver never serves another.
	Cwd string
	// Command returns the agent process for cwd, not started, living as long
	// as ctx. In agentd it is sandbox.BashExecutor.AgentCommand, so the agent
	// runs in the egress jail with cwd bound.
	Command func(ctx context.Context, cwd string) (*exec.Cmd, error)
	// AuthMethod, when set, is the method id sent to authenticate after
	// initialize. opencode and cline sign in outside ACP and leave it empty.
	AuthMethod string
}

// Request is one call: a new session in the driver's Cwd and one prompt turn.
type Request struct {
	// Prompt is plain text: cline accepts no resource blocks.
	Prompt string
	// MCPServers go to session/new.
	MCPServers []acpsdk.McpServer
	// Permit answers the agent's permission requests; nil refuses them all.
	Permit Permitter
	// OnUpdate, when set, sees every session/update of the turn in order.
	OnUpdate func(acpsdk.SessionUpdate)
	// ReadOnly refuses every write the agent asks permission for, before the
	// Permitter. The agent's own writes are the jail's to confine.
	ReadOnly bool
	// Followup, when set, is asked after each turn that ended well: a prompt
	// it returns is another turn in the session, "" ends the call. It must stop.
	Followup func() string
}

// Result is how the call ended. Text is the agent's streamed message, over
// every turn. Usage is the agent's own count, summed over the turns, when it
// reports one (opencode does, cline not). Evidence counts the tool calls that
// completed and changed the workspace or ran a command, as the agent reported
// them.
type Result struct {
	SessionID  string
	Text       string
	StopReason acpsdk.StopReason
	Usage      *acpsdk.Usage
	Evidence   int
}

// Driver keeps one agent process warm and opens a session on it per Call.
// Calls may run concurrently; a process that died is replaced on the next one.
type Driver struct {
	cfg  Config
	mu   sync.Mutex
	proc *process
}

// NewDriver returns a Driver that starts its agent on the first Call.
func NewDriver(cfg Config) *Driver {
	return &Driver{cfg: cfg}
}

// Call runs req in a new session. A ctx cancelled mid-prompt sends
// session/cancel and returns ctx's error; the process stays warm.
func (d *Driver) Call(ctx context.Context, req Request) (Result, error) {
	p, err := d.process(ctx)
	if err != nil {
		return Result{}, err
	}
	servers := req.MCPServers
	if servers == nil {
		servers = []acpsdk.McpServer{}
	}
	created, err := p.conn.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: d.cfg.Cwd, McpServers: servers})
	if err != nil {
		return Result{}, p.fail(ctx, "session/new", err)
	}
	// shortcut: sessions are never closed (session/close is optional and cline lacks it); close them if a warm agent's memory grows.
	s := p.client.open(ctx, created.SessionId, req)
	defer p.client.close(created.SessionId)
	result := Result{SessionID: string(created.SessionId)}
	for prompt := req.Prompt; ; {
		err := p.turn(ctx, s, prompt, &result)
		if err != nil || req.Followup == nil || result.StopReason != acpsdk.StopReasonEndTurn {
			return result, err
		}
		if prompt = req.Followup(); prompt == "" {
			return result, nil
		}
	}
}

// turn sends one prompt in s and adds how it ended to result.
func (p *process) turn(ctx context.Context, s *session, prompt string, result *Result) error {
	s.mu.Lock()
	s.refused = ""
	s.mu.Unlock()
	turn, err := p.conn.Prompt(s.ctx, acpsdk.PromptRequest{SessionId: s.id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock(prompt)}})
	result.Text, result.Evidence, result.StopReason = s.message(), s.evidence(), turn.StopReason
	result.Usage = addUsage(result.Usage, turn.Usage)
	if reason := s.suspended(); reason != "" {
		return fmt.Errorf("%w: %s", ErrSuspended, reason)
	}
	if err != nil {
		return p.fail(ctx, "session/prompt", err)
	}
	// agentd's own cancel returned ctx's error above, so the agent stopped itself.
	if turn.StopReason == acpsdk.StopReasonCancelled {
		if reason := s.refusal(); reason != "" {
			return fmt.Errorf("%w: %s", ErrPermissionRefused, reason)
		}
		return errors.New("acp: the agent cancelled its turn")
	}
	return nil
}

// Close stops the agent process.
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.proc != nil {
		d.proc.kill()
		d.proc = nil
	}
	return nil
}

// process returns the warm process, starting and initializing one when there
// is none or the last one died.
func (d *Driver) process(ctx context.Context) (*process, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.proc != nil && !d.proc.dead() {
		return d.proc, nil
	}
	if d.proc != nil {
		d.proc.kill()
		d.proc = nil
	}
	p, err := start(d.cfg)
	if err != nil {
		return nil, err
	}
	if err := p.handshake(ctx, d.cfg.AuthMethod); err != nil {
		p.kill()
		return nil, err
	}
	d.proc = p
	return p, nil
}

type process struct {
	conn   *acpsdk.ClientSideConnection
	client *client
	stop   context.CancelFunc
	exited chan struct{}
	// waitErr is cmd.Wait's result, set before exited closes.
	waitErr error
}

func start(cfg Config) (*process, error) {
	if cfg.Command == nil {
		return nil, errors.New("acp: no agent command configured")
	}
	ctx, stop := context.WithCancel(context.Background())
	p, err := spawn(ctx, cfg)
	if err != nil {
		stop()
		return nil, err
	}
	p.stop = stop
	return p, nil
}

func spawn(ctx context.Context, cfg Config) (*process, error) {
	cmd, err := cfg.Command(ctx, cfg.Cwd)
	if err != nil {
		return nil, fmt.Errorf("acp: agent command: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: agent stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: agent stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start agent: %w", err)
	}
	p := &process{client: &client{sessions: map[acpsdk.SessionId]*session{}}, exited: make(chan struct{})}
	p.conn = acpsdk.NewClientSideConnection(p.client, stdin, stdout)
	go func() {
		// Wait closes stdout once the agent exits, so a dying agent's last line
		// may be lost; its call fails on the exit either way.
		p.waitErr = cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

// handshake runs initialize, pinned to the SDK's protocol version, then
// authenticate when a method is configured.
func (p *process) handshake(ctx context.Context, authMethod string) error {
	resp, err := p.conn.Initialize(ctx, acpsdk.InitializeRequest{ProtocolVersion: acpsdk.ProtocolVersionNumber})
	if err != nil {
		return p.fail(ctx, "initialize", err)
	}
	if resp.ProtocolVersion != acpsdk.ProtocolVersionNumber {
		return fmt.Errorf("%w at initialize: the agent speaks version %d, agentd pins %d",
			ErrProtocolVersion, resp.ProtocolVersion, acpsdk.ProtocolVersionNumber)
	}
	if authMethod == "" {
		return nil
	}
	if _, err := p.conn.Authenticate(ctx, acpsdk.AuthenticateRequest{MethodId: authMethod}); err != nil {
		return p.fail(ctx, "authenticate", err)
	}
	return nil
}

func (p *process) dead() bool {
	select {
	case <-p.conn.Done():
		return true
	case <-p.exited:
		return true
	default:
		return false
	}
}

func (p *process) kill() {
	p.stop()
	<-p.exited
}

// fail names why op failed: the caller cancelled, the process died, the
// agent's provider refused for quota or rate (the breaker and defer path, the
// same as a gateway 429), or the agent's own error.
func (p *process) fail(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("acp: %s: %w", op, ctx.Err())
	}
	if p.dead() {
		select {
		case <-p.exited:
			return fmt.Errorf("acp: agent process exited during %s: %v", op, p.waitErr)
		case <-time.After(time.Second):
			return fmt.Errorf("acp: agent process exited during %s: %w", op, err)
		}
	}
	if quotaError.MatchString(err.Error()) {
		return fmt.Errorf("%w: acp agent: %s: %v", models.ErrLLMQuotaExceeded, op, err)
	}
	return fmt.Errorf("acp: %s: %w", op, err)
}
