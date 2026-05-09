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

// TestStreamFiltersByTaskID asserts that ?task_id=... narrows the
// subscription to the matching topic and ignores unrelated signals.
func TestStreamFiltersByTaskID(t *testing.T) {
	eventBus := bus.NewInProcess()
	server := httptest.NewServer(Handler{Bus: eventBus, Hub: &Hub{}})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?task_id=abc", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	time.Sleep(20 * time.Millisecond)
	eventBus.Publish(context.Background(), bus.Signal{Topic: "task:other", Type: "FAILURE", Payload: "ignored"})
	eventBus.Publish(context.Background(), bus.Signal{Topic: "task:abc", Type: "RESULT", Payload: "kept"})

	reader := bufio.NewReader(resp.Body)
	type frame struct {
		event string
		data  string
	}
	deadline := time.Now().Add(time.Second)
	var got frame
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			got.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			got.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if got.data != "" {
				goto done
			}
		}
	}
done:
	if got.event != "task_updated" {
		t.Fatalf("event = %q, want task_updated", got.event)
	}
	if !strings.Contains(got.data, "task:abc") || !strings.Contains(got.data, "kept") {
		t.Fatalf("data = %q", got.data)
	}
}

// TestStreamWritesEventNames asserts named SSE event lines are emitted.
func TestStreamWritesEventNames(t *testing.T) {
	eventBus := bus.NewInProcess()
	server := httptest.NewServer(Handler{Bus: eventBus, Hub: &Hub{}})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	time.Sleep(20 * time.Millisecond)

	eventBus.Publish(context.Background(), bus.Signal{Topic: bus.GlobalTopic, Type: "agent_updated", Payload: `{"id":"qa"}`})

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(time.Second)
	var sawEventLine bool
	for time.Now().Before(deadline) && !sawEventLine {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.TrimRight(line, "\n") == "event: agent_updated" {
			sawEventLine = true
		}
	}
	if !sawEventLine {
		t.Fatal("missing 'event: agent_updated' line")
	}
}
