package queue

import (
	"context"
	"strings"
	"sync"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

type queueGateway struct {
	content string
	err     error
}

func (g *queueGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{Content: g.content}, g.err
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
		select {
		case <-ctx.Done():
			s.cancelOnce.Do(func() { close(s.cancelled) })
			return sandbox.Result{Success: false, ExitCode: -1}, ctx.Err()
		default:
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
