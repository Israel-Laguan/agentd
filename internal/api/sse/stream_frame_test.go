package sse

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/bus"
	"agentd/internal/models"
)

func frameData(t *testing.T, sig bus.Signal) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	if !writeEvent(rec, sig) {
		t.Fatal("writeEvent returned false")
	}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var got map[string]string
			if err := json.Unmarshal([]byte(data), &got); err != nil {
				t.Fatalf("data line is not JSON: %v", err)
			}
			return got
		}
	}
	t.Fatalf("no data line in %q", rec.Body.String())
	return nil
}

// TestWriteEventNamesTaskAndProject asserts a global-stream frame says which
// task and project it belongs to, so a subscriber need not parse the payload.
func TestWriteEventNamesTaskAndProject(t *testing.T) {
	got := frameData(t, bus.Signal{
		Topic: bus.GlobalTopic, Type: "RESULT", Payload: "{}", TaskID: "t-1", ProjectID: "p-1",
	})
	if got["task_id"] != "t-1" || got["project_id"] != "p-1" {
		t.Fatalf("frame = %v, want task_id t-1 and project_id p-1", got)
	}
	if got["topic"] != bus.GlobalTopic || got["type"] != "RESULT" || got["payload"] != "{}" {
		t.Fatalf("existing envelope fields changed: %v", got)
	}
}

// TestWriteEventOmitsEmptyIDs keeps frames without a task (agent events) free
// of empty-string IDs.
func TestWriteEventOmitsEmptyIDs(t *testing.T) {
	got := frameData(t, bus.Signal{Topic: bus.GlobalTopic, Type: "agent_updated", Payload: "{}"})
	if _, ok := got["task_id"]; ok {
		t.Fatalf("frame = %v, want no task_id", got)
	}
	if _, ok := got["project_id"]; ok {
		t.Fatalf("frame = %v, want no project_id", got)
	}
}

// TestEventNameForBroadcastTaskUpdate keeps the SSE name the web board
// subscribes to in step with the broadcast-only TASK_UPDATED event type.
func TestEventNameForBroadcastTaskUpdate(t *testing.T) {
	if got := eventName(string(models.EventTypeTaskUpdated)); got != "task_updated" {
		t.Fatalf("eventName = %q, want task_updated", got)
	}
}
