package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/models"
	acpsdk "github.com/coder/acp-go-sdk"
)

// fakeTokenStore records AddTokenUsage calls for test assertions.
type fakeTokenStore struct {
	mu           sync.Mutex
	calls        []tokenUsageCall
	detailsCalls []gateway.UsageDetails
}

type tokenUsageCall struct {
	taskID string
	tokens int
}

func (s *fakeTokenStore) AddTokenUsage(_ context.Context, taskID string, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, tokenUsageCall{taskID: taskID, tokens: tokens})
	return nil
}

// AddUsageDetails records prompt-cache usage details for test assertions.
func (s *fakeTokenStore) AddUsageDetails(_ context.Context, _ string, details gateway.UsageDetails) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detailsCalls = append(s.detailsCalls, details)
	return nil
}

func (s *fakeTokenStore) totalTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, c := range s.calls {
		total += c.tokens
	}
	return total
}

func (s *fakeTokenStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// ACP usage is recorded once for the task even without a rolling budget.
func TestACPTokenUsageIsRecordedAndEmitted(t *testing.T) {
	step := report(map[string]any{"summary": "done"})
	step.Usage = &acpsdk.Usage{TotalTokens: 15}
	w, store, task, project, profile, _, _ := acpFixture(t, fakeagent.Script(step), false)
	tokens := &fakeTokenStore{}
	sink := &mockEventSink{}
	w.tokenStore, w.sink = tokens, sink
	w.dispatchProcess(context.Background(), task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if tokens.totalTokens() != 15 || tokens.callCount() != 1 || tokens.calls[0].taskID != task.ID {
		t.Fatalf("token calls = %+v, want 15 for task %s", tokens.calls, task.ID)
	}
	for _, event := range sink.events {
		if event.Type == models.EventTypeTokenUsage && event.Payload == `{"tokens":15}` {
			return
		}
	}
	t.Fatalf("events = %+v, want TOKEN_USAGE for 15", sink.events)
}

func TestACPTokenRecordingCanBeDisabled(t *testing.T) {
	step := report(map[string]any{"summary": "done"})
	step.Usage = &acpsdk.Usage{TotalTokens: 15}
	w, _, task, project, profile, _, _ := acpFixture(t, fakeagent.Script(step), false)
	tokens := &fakeTokenStore{}
	w.tokenStore, w.disableTokenRecording = tokens, true
	w.dispatchProcess(context.Background(), task, project, profile)
	if tokens.callCount() != 0 {
		t.Fatalf("token calls = %d, want 0", tokens.callCount())
	}
}

// The shared recorder preserves cache details when an agent reports them.
func TestTokenUsagePreservesCacheDetails(t *testing.T) {
	tokens := &fakeTokenStore{}
	sink := &mockEventSink{}
	w := &Worker{tokenStore: tokens, sink: sink}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task"}}
	w.RecordTaskTokenUsage(context.Background(), task, 15, gateway.UsageDetails{CachedTokens: 8, CacheWriteTokens: 3})
	if len(tokens.detailsCalls) != 1 || tokens.detailsCalls[0].CachedTokens != 8 || tokens.detailsCalls[0].CacheWriteTokens != 3 {
		t.Fatalf("details = %+v", tokens.detailsCalls)
	}
	for _, event := range sink.events {
		if event.Type == models.EventTypeTokenUsage && event.Payload == `{"tokens":15,"cached_tokens":8,"cache_write_tokens":3}` {
			return
		}
	}
	t.Fatalf("events = %+v, want cache details", sink.events)
}

func TestTokenUsageAPIResponseIncludesField(t *testing.T) {
	raw, err := json.Marshal(models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, TokenUsage: 42})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if value["token_usage"] != float64(42) {
		t.Fatalf("token_usage = %v, want 42", value["token_usage"])
	}
}
