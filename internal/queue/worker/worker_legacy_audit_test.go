package worker

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/models"
)

// newLegacyAuditTest builds a Worker backed by the standard routing-test mocks
// and an enabled AuditLogger writing to path.
func newLegacyAuditTest(t *testing.T, auditPath string) (*Worker, *routingTestStore) {
	t.Helper()
	profile := models.AgentProfile{
		ID:          "agent-legacy-audit",
		Provider:    "ollama",
		Model:       "llama3",
		AgenticMode: false,
	}
	store := &routingTestStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task-legacy-audit"},
			ProjectID:  "proj-audit",
			AgentID:    "agent-legacy-audit",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "proj-audit"},
			WorkspacePath: t.TempDir(),
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Audit: config.AuditConfig{
			Enabled: true,
			Path:    auditPath,
		},
	})
	return w, store
}

// readAuditRecords reads all JSONL records from path.
func readAuditRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal audit line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

func TestRunLegacyTask_AuditEventsWritten(t *testing.T) {
	t.Parallel()
	auditPath := filepathJoinTemp(t, "audit.jsonl")
	w, store := newLegacyAuditTest(t, auditPath)

	w.Process(context.Background(), store.task)

	records := readAuditRecords(t, auditPath)

	var hasStart, hasEnd bool
	for _, rec := range records {
		rt, _ := rec["record_type"].(string)
		switch rt {
		case recordTypeTaskStart:
			hasStart = true
			if rec["type"] != recordTypeTaskStart {
				t.Errorf("task_start: type = %v, want %q", rec["type"], recordTypeTaskStart)
			}
			if rec["task_id"] != "task-legacy-audit" {
				t.Errorf("task_start: task_id = %v, want task-legacy-audit", rec["task_id"])
			}
			if rec["project_id"] != "proj-audit" {
				t.Errorf("task_start: project_id = %v, want proj-audit", rec["project_id"])
			}
			if rec["provider"] != "ollama" {
				t.Errorf("task_start: provider = %v, want ollama", rec["provider"])
			}
			if _, ok := rec["timestamp"]; !ok {
				t.Error("task_start: missing timestamp")
			}
		case recordTypeTaskComplete, recordTypeTaskFail:
			hasEnd = true
			if rec["type"] != rt {
				t.Errorf("end record: type = %v, want %q", rec["type"], rt)
			}
			if rec["task_id"] != "task-legacy-audit" {
				t.Errorf("end record: task_id = %v, want task-legacy-audit", rec["task_id"])
			}
			if rec["provider"] != "ollama" {
				t.Errorf("end record: provider = %v, want ollama", rec["provider"])
			}
			if _, ok := rec["timestamp"]; !ok {
				t.Error("end record: missing timestamp")
			}
		}
	}
	if !hasStart {
		t.Errorf("no task_start record found in: %v", records)
	}
	if !hasEnd {
		t.Errorf("no task_complete or task_fail record found in: %v", records)
	}
}

func TestRunLegacyTask_AuditComplete_HasCommand(t *testing.T) {
	t.Parallel()
	auditPath := filepathJoinTemp(t, "audit-cmd.jsonl")
	w, store := newLegacyAuditTest(t, auditPath)

	w.Process(context.Background(), store.task)

	records := readAuditRecords(t, auditPath)
	for _, rec := range records {
		rt, _ := rec["record_type"].(string)
		if rt == recordTypeTaskComplete {
			// The mock gateway returns {"command":"echo ok"} so command should be set.
			if rec["command"] == "" || rec["command"] == nil {
				t.Errorf("task_complete missing command field: %v", rec)
			}
			if _, ok := rec["token_usage"]; !ok {
				t.Errorf("task_complete missing token_usage field: %v", rec)
			}
			return
		}
	}
	t.Error("no task_complete record found")
}

func TestRunLegacyTask_AuditDisabled_NoFile(t *testing.T) {
	t.Parallel()
	auditPath := filepathJoinTemp(t, "should-not-exist.jsonl")

	profile := models.AgentProfile{
		ID:          "agent-no-audit",
		Provider:    "ollama",
		Model:       "llama3",
		AgenticMode: false,
	}
	store := &routingTestStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task-no-audit"},
			ProjectID:  "proj-1",
			AgentID:    "agent-no-audit",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "proj-1"},
			WorkspacePath: t.TempDir(),
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Audit: config.AuditConfig{
			Enabled: false,
			Path:    auditPath,
		},
	})

	w.Process(context.Background(), store.task)

	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Errorf("expected no audit file with audit disabled; stat err = %v", err)
	}
}

func TestRunLegacyTask_AuditAppend_MultipleTasksValidJSONL(t *testing.T) {
	t.Parallel()
	auditPath := filepathJoinTemp(t, "audit-multi.jsonl")

	// Run two tasks sequentially using the same audit file.
	for i := 0; i < 2; i++ {
		profile := models.AgentProfile{
			ID:          "agent-multi",
			Provider:    "ollama",
			Model:       "llama3",
			AgenticMode: false,
		}
		store := &routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-multi"},
				ProjectID:  "proj-multi",
				AgentID:    "agent-multi",
				State:      models.TaskStateQueued,
			},
			project: models.Project{
				BaseEntity:    models.BaseEntity{ID: "proj-multi"},
				WorkspacePath: t.TempDir(),
			},
			profile: profile,
		}
		gw := &routingTestGateway{}
		sb := &routingTestSandbox{}
		w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
			MaxToolIterations: 5,
			Audit: config.AuditConfig{
				Enabled: true,
				Path:    auditPath,
			},
		})
		w.Process(context.Background(), store.task)
	}

	// Every line must be valid JSON.
	records := readAuditRecords(t, auditPath)
	if len(records) < 4 { // at least 2 starts + 2 ends
		t.Errorf("expected at least 4 audit records for 2 tasks, got %d", len(records))
	}
}
