package queue

import (
	"context"
	"strings"
	"sync"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type queueGateway struct {
	content   string
	toolCalls []gateway.ToolCall
	err       error
}

// Generate answers the first request of a turn with toolCalls, then content once
// the tool results are back. Without toolCalls every request gets content.
func (g *queueGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	if len(g.toolCalls) > 0 && !lastMessageIsToolResult(req) {
		return gateway.AIResponse{ToolCalls: g.toolCalls}, g.err
	}
	return gateway.AIResponse{Content: g.content}, g.err
}

func (g *queueGateway) ProviderSupportsChatTools(provider string) bool {
	return testutil.ProviderSupportsChatTools(provider)
}

func lastMessageIsToolResult(req gateway.AIRequest) bool {
	return len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool"
}

func (g *queueGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, g.err
}

func (*queueGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (*queueGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}
func (*queueGateway) Embed(ctx context.Context, req gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

type queueSandbox struct {
	result      sandbox.Result
	err         error
	blockOnCtx  bool
	started     chan struct{}
	cancelled   chan struct{}
	unblock     chan struct{}
	startedOnce sync.Once
	cancelOnce  sync.Once
	unblockOnce sync.Once
}

func (s *queueSandbox) Execute(ctx context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	if !s.blockOnCtx {
		return s.result, s.err
	}
	s.startedOnce.Do(func() { close(s.started) })
	if s.unblock != nil {
		if err := ctx.Err(); err != nil {
			s.cancelOnce.Do(func() { close(s.cancelled) })
			return sandbox.Result{Success: false, ExitCode: -1}, err
		}
		select {
		case <-ctx.Done():
			s.cancelOnce.Do(func() { close(s.cancelled) })
			return sandbox.Result{Success: false, ExitCode: -1}, ctx.Err()
		case <-s.unblock:
			return s.result, s.err
		}
	}
	<-ctx.Done()
	s.cancelOnce.Do(func() { close(s.cancelled) })
	return sandbox.Result{Success: false, ExitCode: -1}, ctx.Err()
}

func (s *queueSandbox) unblockProbe() {
	if s.unblock == nil {
		return
	}
	s.unblockOnce.Do(func() { close(s.unblock) })
}

type queueSink struct {
	mu     sync.Mutex
	events []models.Event
}

func (s *queueSink) Emit(_ context.Context, evt models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
	return nil
}

func (s *queueSink) contains(payload string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, evt := range s.events {
		if strings.Contains(evt.Payload, payload) {
			return true
		}
	}
	return false
}

func (s *queueSink) containsType(eventType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, evt := range s.events {
		if string(evt.Type) == eventType {
			return true
		}
	}
	return false
}
