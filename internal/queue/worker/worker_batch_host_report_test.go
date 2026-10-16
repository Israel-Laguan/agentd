package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

// completeHostFacts carries every core fact the renderer requires (B-046).
var completeHostFacts = map[string]string{
	"os_info.txt":     "Operating System: Debian GNU/Linux 13 (trixie)\n      Machine ID: 9ebbf53566051e82c3e15b536a381807\n          Kernel: Linux 7.2.9-2-liquorix-amd64\n",
	"cpu_info.txt":    "Model name:                              Intel(R) Core(TM) Ultra 5 226V\nCPU(s):                                  8\n",
	"memory_info.txt": "               total        used\nMem:            15Gi       7.9Gi\n",
	"disk_info.txt":   "NAME        MAJ:MIN RM   SIZE RO TYPE MOUNTPOINTS\nnvme0n1     259:0    0 476.9G  0 disk\n",
}

// B-050: a host report task claimed in a legacy batch is rendered from the
// collected files like on the single-task path, instead of running the
// command the batch model wrote for its slot.
func TestProcessBatchLegacyRendersHostReportTask(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	for name, content := range completeHostFacts {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: workspace},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4"},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(3)
	tasks[2].Title = "Build report"
	tasks[2].Description = "Create the system report.md summarizing the OS and hardware from the collected files"
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	gw := &batchTrackingGateway{}
	sb := &batchCountingSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{Batching: config.BatchingConfig{Enabled: true, MaxBatchSize: 5}})

	w.ProcessBatch(context.Background(), tasks)

	sb.mu.Lock()
	calls := sb.calls
	sb.mu.Unlock()
	if calls != 2 {
		t.Fatalf("sandbox calls = %d, want 2: the report task must not run a model-written command", calls)
	}
	report, err := os.ReadFile(filepath.Join(workspace, "report.md"))
	if err != nil {
		t.Fatalf("report.md not rendered: %v", err)
	}
	if !strings.Contains(string(report), "Debian GNU/Linux 13 (trixie)") || strings.Contains(string(report), "9ebbf535") {
		t.Fatalf("report.md = %q, want rendered facts without the machine id", report)
	}
	if res := store.results[tasks[2].ID]; res == nil || !res.Success {
		t.Fatalf("report task result = %#v, want a successful commit", res)
	}
}
