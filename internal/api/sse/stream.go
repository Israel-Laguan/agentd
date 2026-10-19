package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
	"agentd/internal/bus"
	"agentd/internal/models"
)

const subscribeBuffer = 32

// Handler implements GET /api/v1/events/stream as a Server-Sent Events
// firehose. Clients can narrow the stream with task_id and project_id
// query parameters; when omitted, every signal published on the bus is
// forwarded.
type Handler struct {
	Bus bus.Bus
	Hub *Hub
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Bus == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "event bus is unavailable")
		return
	}
	hub := h.Hub
	if hub == nil {
		hub = &Hub{}
	}

	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))

	topic := bus.GlobalTopic
	switch {
	case taskID != "":
		topic = "task:" + taskID
	case projectID != "":
		topic = "project:" + projectID
	}

	events, unsubscribe := h.Bus.Subscribe(topic, subscribeBuffer)
	defer unsubscribe()
	hub.Add()
	defer hub.Done()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	_ = http.NewResponseController(w).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if !writeEvent(w, evt) {
				return
			}
		}
	}
}

// frameEnvelope is the "data:" JSON of one frame. task_id and project_id are
// additive: clients that ignore them keep working, and signals that belong to
// no task omit them.
type frameEnvelope struct {
	Topic     string `json:"topic"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	TaskID    string `json:"task_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// writeEvent serializes a Signal into a single SSE frame. The "event:"
// line carries the cockpit-friendly lowercased event name (so EventSource
// listeners can subscribe to "task_failed", "log_chunk", etc.) while the
// "data:" line keeps the same JSON shape we always emitted for
// backwards compatibility with clients that ignore the event line.
func writeEvent(w http.ResponseWriter, evt bus.Signal) bool {
	data, err := json.Marshal(frameEnvelope{
		Topic: evt.Topic, Type: evt.Type, Payload: evt.Payload, TaskID: evt.TaskID, ProjectID: evt.ProjectID,
	})
	if err != nil {
		return false
	}
	if name := eventName(evt.Type); name != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
			return false
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	_ = http.NewResponseController(w).Flush()
	return true
}

// eventName maps internal event identifiers to the dashed/lowercased names
// SSE clients subscribe to. Unknown types return "" which causes the
// caller to omit the "event:" line (clients then receive a default
// "message" event).
func eventName(t string) string {
	if t == "" {
		return ""
	}
	switch models.EventType(t) {
	case models.EventTypeLog:
		return "log_chunk"
	case models.EventTypeFailure:
		return "task_failed"
	case models.EventTypeResult, models.EventTypeTaskUpdated:
		return "task_updated"
	case models.EventTypeComment, models.EventTypeCommentIntake:
		return "comment_added"
	case models.EventTypeRecovery, models.EventTypeRebootRecovery, models.EventTypeRebootRecoveryHandoff:
		return "task_recovered"
	case models.EventTypeHeartbeatReconcile:
		return "task_reconciled"
	case models.EventTypeToolCall:
		return "tool_called"
	case models.EventTypeToolResult:
		return "tool_result"
	}
	switch t {
	case "agent_updated", "agent_deleted", "task_assigned", "task_split", "task_retried":
		return t
	}
	return strings.ToLower(t)
}
