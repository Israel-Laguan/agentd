package agentic

import (
	"context"
	"errors"
	"testing"

	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// These tests pin that the engine writes nothing to the board on each way Process
// ends. S19 PR 01 moves those writes to the worker ("only the core writes the
// board", T-041); the outcomes they pin must survive that move unchanged.

// boardWriteHost records the Host methods that change the board.
type boardWriteHost struct {
	*topicDriftHost
	sessionStartErr error
	failedWith      []error
	gatewayErrs     []error
	commits         int
}

func (h *boardWriteHost) CommitTextWithProfile(ctx context.Context, task models.Task, text string, profile *models.AgentProfile) {
	h.commits++
	h.topicDriftHost.CommitTextWithProfile(ctx, task, text, profile)
}

func (h *boardWriteHost) RunSessionStart(context.Context, models.Task, models.Project, *agenthooks.HookChain) error {
	return h.sessionStartErr
}

func (h *boardWriteHost) FailHard(_ context.Context, _ models.Task, err error) {
	h.failedWith = append(h.failedWith, err)
}

func (h *boardWriteHost) HandleGatewayError(_ context.Context, _ models.Task, err error) {
	h.gatewayErrs = append(h.gatewayErrs, err)
}

// scriptedGateway answers every call with err, or with "done" when err is nil,
// and reports tool support as configured.
type scriptedGateway struct {
	err   error
	tools bool
	calls int
}

func (g *scriptedGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	g.calls++
	if g.err != nil {
		return gateway.AIResponse{}, g.err
	}
	return gateway.AIResponse{Content: "done"}, nil
}

func (g *scriptedGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
}

func (g *scriptedGateway) ProviderSupportsChatTools(string) bool { return g.tools }

func runProcessForBoardWrites(t *testing.T, host *boardWriteHost, gw *scriptedGateway) bool {
	t.Helper()
	e := NewEngine(Config{Store: testutil.NewFakeStore(), Gateway: gw}, host)
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-1"}, ProjectID: "project-1", AgentID: "agent-1", Title: "t"}
	project := models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: t.TempDir()}
	profile := models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	_, ok := e.Process(context.Background(), task, project, profile)
	return ok
}

func newBoardWriteHost() *boardWriteHost {
	return &boardWriteHost{topicDriftHost: &topicDriftHost{noopHost: &noopHost{}, committed: new(string)}}
}

func TestProcessFailsHardWhenSessionStartFails(t *testing.T) {
	hookErr := errors.New("credential check failed")
	host := newBoardWriteHost()
	host.sessionStartErr = hookErr
	gw := &scriptedGateway{tools: true}

	if runProcessForBoardWrites(t, host, gw) {
		t.Fatal("Process reported a result after the session-start hooks failed")
	}
	if len(host.failedWith) != 0 {
		t.Fatalf("FailHard calls = %v, want none: the engine must not write the board", host.failedWith)
	}
	if gw.calls != 0 || *host.committed != "" {
		t.Fatalf("the task ran after a failed session start: model calls=%d committed=%q", gw.calls, *host.committed)
	}
}

func TestProcessFailsHardWithoutChatToolSupport(t *testing.T) {
	host := newBoardWriteHost()
	gw := &scriptedGateway{tools: false}

	if runProcessForBoardWrites(t, host, gw) {
		t.Fatal("Process reported a result for a provider without tool support")
	}
	if len(host.failedWith) != 0 {
		t.Fatalf("FailHard calls = %v, want none: the engine must not write the board", host.failedWith)
	}
	if gw.calls != 0 {
		t.Fatalf("model calls = %d, want 0", gw.calls)
	}
}

func TestProcessHandsAGatewayErrorToTheHost(t *testing.T) {
	host := newBoardWriteHost()
	gw := &scriptedGateway{tools: true, err: models.ErrLLMUnreachable}

	if runProcessForBoardWrites(t, host, gw) {
		t.Fatal("Process reported a result after the model call failed")
	}
	if len(host.gatewayErrs) != 0 {
		t.Fatalf("HandleGatewayError calls = %v, want none: the engine must not write the board", host.gatewayErrs)
	}
	if len(host.failedWith) != 0 || *host.committed != "" {
		t.Fatalf("a gateway error also failed or committed the task: failed=%v committed=%q", host.failedWith, *host.committed)
	}
}

func TestProcessCommitsTheFinalText(t *testing.T) {
	host := newBoardWriteHost()
	gw := &scriptedGateway{tools: true}

	if !runProcessForBoardWrites(t, host, gw) {
		t.Fatal("Process reported no result for a run that answered")
	}
	if host.commits != 0 || *host.committed != "" {
		t.Fatalf("the engine committed the text itself: calls=%d committed=%q", host.commits, *host.committed)
	}
	if len(host.failedWith) != 0 || len(host.gatewayErrs) != 0 {
		t.Fatalf("a finished run also failed: failed=%v gateway errors=%v", host.failedWith, host.gatewayErrs)
	}
}
