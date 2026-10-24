package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// agenticHostGateway advertises tool support, so a profile on it takes the
// agentic path instead of the legacy fallback that would render the report.
type agenticHostGateway struct {
	routingTestGateway
}

func (g *agenticHostGateway) ProviderSupportsChatTools(provider string) bool {
	return provider != ""
}

// B-045 on the agentic path: a report task on an agentic profile is rendered
// from the collected facts by code. The model never writes the report.
func TestDispatchRendersHostReportOnAgenticProfile(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task, project := seedHostReportTask(t, store, "Create system report.md summarizing the OS and hardware from the collected files", completeHostFacts)
	gw := &agenticHostGateway{}
	w := NewWorker(store, gw, panicSandbox{t}, nil, &mockEventSink{}, WorkerOptions{})

	w.dispatchProcess(ctx, task, project, models.AgentProfile{AgenticMode: true, Provider: "openai"})

	if len(gw.requests) != 0 {
		t.Fatalf("gateway called %d times, want 0: the model must not write the report", len(gw.requests))
	}
	got, err := os.ReadFile(filepath.Join(project.WorkspacePath, "report.md"))
	if err != nil {
		t.Fatalf("report.md not written: %v", err)
	}
	if !strings.Contains(string(got), "Debian GNU/Linux 13 (trixie)") {
		t.Fatalf("report lacks the OS fact:\n%s", got)
	}
	if after, _ := store.GetTask(ctx, task.ID); after.State != models.TaskStateCompleted {
		t.Fatalf("task state = %s, want COMPLETED", after.State)
	}
}

// agenticBatchRecorder answers every slot of a batch and keeps the user prompts
// it was sent, so a test can see which tasks reached the model.
type agenticBatchRecorder struct {
	agenticHostGateway
	prompts []string
}

func (g *agenticBatchRecorder) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	user := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	g.prompts = append(g.prompts, user)
	slots := make([]string, 0, countBatchSlots(user))
	for slot := range countBatchSlots(user) {
		slots = append(slots, fmt.Sprintf(`{"slot":%d,"content":"done"}`, slot))
	}
	return gateway.AIResponse{Content: `{"results":[` + strings.Join(slots, ",") + `]}`}, nil
}

// B-050 on the agentic path: a report task claimed in an agentic batch is
// rendered from the collected files and never reaches the batch prompt.
func TestProcessBatchAgenticRendersHostReportTask(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	for name, content := range completeHostFacts {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: workspace},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: true},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	tasks[2].Title = "Build report"
	tasks[2].Description = "Create the system report.md summarizing the OS and hardware from the collected files"
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &agenticBatchRecorder{}
	w := NewWorker(store, gw, &batchCountingSandbox{}, nil, nil, WorkerOptions{Batching: config.BatchingConfig{Enabled: true, MaxBatchSize: 5}})

	w.ProcessBatch(context.Background(), tasks)

	report, err := os.ReadFile(filepath.Join(workspace, "report.md"))
	if err != nil {
		t.Fatalf("report.md not rendered: %v", err)
	}
	if !strings.Contains(string(report), "Debian GNU/Linux 13 (trixie)") || strings.Contains(string(report), "9ebbf535") {
		t.Fatalf("report.md = %q, want rendered facts without the machine id", report)
	}
	for _, prompt := range gw.prompts {
		if strings.Contains(prompt, tasks[2].Description) {
			t.Fatalf("report task reached the batch prompt; the model must not write it:\n%s", prompt)
		}
	}
	if res := store.results[tasks[2].ID]; res == nil || !res.Success {
		t.Fatalf("report task result = %#v, want a successful commit", res)
	}
}
