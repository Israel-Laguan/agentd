package worker

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// B-054: TOKEN_USAGE names the model that answered, and keeps the "tokens" key
// existing readers use; without a known model the key is omitted.
func TestRecordTaskTokenUsageNamesAnsweringModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		model     string
		wantModel any
	}{
		{"model known", "poolside/laguna-s-2.1", "poolside/laguna-s-2.1"},
		{"model unknown", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &mockEventSink{}
			w := &Worker{sink: sink}
			w.RecordTaskTokenUsage(context.Background(), models.Task{}, 92, gateway.UsageDetails{Model: tt.model})
			if len(sink.events) != 1 || sink.events[0].Type != models.EventTypeTokenUsage {
				t.Fatalf("events = %#v, want one TOKEN_USAGE", sink.events)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(sink.events[0].Payload), &payload); err != nil {
				t.Fatalf("payload %q: %v", sink.events[0].Payload, err)
			}
			if payload["tokens"] != float64(92) || payload["model"] != tt.wantModel {
				t.Fatalf("payload = %v, want tokens 92 and model %v", payload, tt.wantModel)
			}
		})
	}
}
