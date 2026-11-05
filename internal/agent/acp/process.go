package acp

import (
	"context"
	"errors"
	"fmt"

	acpsdk "github.com/coder/acp-go-sdk"
)

type process struct {
	conn   *acpsdk.ClientSideConnection
	client *client
	stop   context.CancelFunc
	exited chan struct{}
	// waitErr is cmd.Wait's result, set before exited closes.
	waitErr error
	// mcpURL is the agentd MCP url this process runs with; the driver restarts
	// a process that reads it from its own config when the url changes.
	mcpURL string
}

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
	if turn.StopReason == acpsdk.StopReasonCancelled {
		if reason := s.refusal(); reason != "" {
			return fmt.Errorf("%w: %s", ErrPermissionRefused, reason)
		}
		return errors.New("acp: the agent cancelled its turn")
	}
	return nil
}

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

func (p *process) idle() bool {
	p.client.mu.Lock()
	defer p.client.mu.Unlock()
	return len(p.client.sessions) == 0
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
	if p.stop != nil {
		p.stop()
	}
	<-p.exited
}