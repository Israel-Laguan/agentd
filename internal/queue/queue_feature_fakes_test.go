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

type queueSandbox struct {
	result      sandbox.Result
	err         error
	blockOnCtx  bool
	started     chan struct{}
	cancelled   chan struct{}
	startedOnce sync.Once
	cancelOnce  sync.Once
}

func (s *queueSandbox) Execute(ctx context.Context, _ sandbox.Payload) (sandbox.Result, error) {
	if !s.blockOnCtx {
		return s.result, s.err
	}
	s.startedOnce.Do(func() { close(s.started) })
	<-ctx.Done()
	s.cancelOnce.Do(func() { close(s.cancelled) })
	return sandbox.Result{Success: false, ExitCode: -1}, ctx.Err()
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
