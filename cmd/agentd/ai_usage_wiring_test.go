package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentd/internal/api/server"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/kanban"
	"agentd/internal/models"
	"agentd/internal/services"
)

// fakeBackend answers every call with the same JSON and a fixed token count.
type fakeBackend struct{ reply string }

func (fakeBackend) Name() spec.Provider { return "fake" }
func (fakeBackend) MaxInputChars() int  { return 0 }
func (fakeBackend) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (b fakeBackend) Generate(context.Context, spec.AIRequest) (spec.AIResponse, error) {
	return spec.AIResponse{Content: b.reply, TokenUsage: 42, ProviderUsed: "fake", ModelUsed: "fake-model"}, nil
}

func openUsageTestStore(t *testing.T) *kanban.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := kanban.OpenStore(filepath.Join(dir, "agentd.db"), dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestAttachCallRecorderNeedsAStoreThatRecords(t *testing.T) {
	gw := gateway.NewRouter(fakeBackend{reply: "{}"})
	if w := attachCallRecorder(gw, nil); w != nil {
		t.Fatal("a nil store got a writer")
	}
	if w := attachCallRecorder(gw, openUsageTestStore(t)); w == nil {
		t.Fatal("the kanban store got no writer")
	}
}

// One real chat request (HTTP in, planner, router, fake provider) writes
// ai_calls rows under its correlation ID, and the report shows them through the
// API and through the CLI. The compose e2e stack needs a podman image build and
// disk this machine lacks, so this is the in-process proof.
func TestChatRequestProducesAICallsAndTheReportShowsThem(t *testing.T) {
	store := openUsageTestStore(t)
	gw := gateway.NewRouter(fakeBackend{reply: `{"intent":"out_of_scope","reason":"fake"}`})
	writer := attachCallRecorder(gw, store)
	handler := server.NewHandler(server.ServerDeps{
		Store: store, Gateway: gw, Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
		FileStash:  &frontdesk.FileStash{Dir: t.TempDir()},
		AIUsage:    services.NewAIUsageService(store, writer),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"messages":[{"role":"user","content":"hello there"}]}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("chat: %v status %v", err, resp)
	}
	_ = resp.Body.Close()
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.Close(closeCtx); err != nil {
		t.Fatalf("flush writer: %v", err)
	}

	rep, err := store.AIUsageReport(context.Background(), models.AIUsageQuery{GroupBy: []string{"purpose", "request"}})
	if err != nil || len(rep.Rows) != 1 {
		t.Fatalf("report = %+v err %v, want one purpose+request row", rep, err)
	}
	row := rep.Rows[0]
	if row.Group["purpose"] != "intake.classify_intent" || row.Group["request"] == "" || row.Calls != 1 || row.Tokens != 42 {
		t.Fatalf("row = %+v, want one classify_intent call of 42 tokens under a correlation ID", row)
	}

	if row.InputChars == 0 || row.MixChars["system"]+row.MixChars["task"] != row.InputChars {
		t.Fatalf("mix = %v for %d input chars, want system and task to account for all of it", row.MixChars, row.InputChars)
	}

	out := runCLI(t, t.TempDir(), "report", "ai", "--api-url", srv.URL, "--group-by", "stage,model")
	for _, want := range []string{"STAGE", "intake", "fake-model", "42", "dropped records: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("CLI output missing %q\n%s", want, out)
		}
	}
}
