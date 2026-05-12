package sse

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentd/internal/bus"
)

func TestStreamPublishesAndCleansUp(t *testing.T) {
	eventBus := bus.NewInProcess()
	hub := &Hub{}
	server := httptest.NewServer(Handler{Bus: eventBus, Hub: hub})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %s", resp.Header.Get("Content-Type"))
	}
	eventBus.Publish(context.Background(), bus.Signal{Topic: bus.GlobalTopic, Type: "LOG", Payload: "hello"})
	frameCh := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(resp.Body)
		var buf strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			buf.WriteString(line)
			if line == "\n" {
				frameCh <- buf.String()
				return
			}
		}
	}()
	select {
	case frame := <-frameCh:
		if !strings.Contains(frame, "event: log_chunk") {
			t.Fatalf("frame missing event line: %q", frame)
		}
		if !strings.Contains(frame, `"type":"LOG"`) {
			t.Fatalf("frame missing data line: %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
	if hub.Active() != 1 {
		t.Fatalf("active = %d, want 1", hub.Active())
	}
	cancel()
	deadline := time.After(time.Second)
	for hub.Active() != 0 {
		select {
		case <-deadline:
			t.Fatalf("active = %d, want 0", hub.Active())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestEventName(t *testing.T) {
	tests := []struct {
		name     string
		eventType string
		want    string
	}{
		{
			name:     "EventTypeToolCall maps to tool_called",
			eventType: "TOOL_CALL",
			want:     "tool_called",
		},
		{
			name:     "EventTypeToolResult maps to tool_result",
			eventType: "TOOL_RESULT",
			want:     "tool_result",
		},
		{
			name:     "unknown event type returns lowercase version",
			eventType: "UNKNOWN_EVENT",
			want:     "unknown_event",
		},
		{
			name:     "empty event type returns empty string",
			eventType: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventName(tt.eventType)
			if got != tt.want {
				t.Errorf("eventName(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}
