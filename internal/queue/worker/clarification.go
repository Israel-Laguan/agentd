package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

// ClarificationMessage describes a structured question the agent sends
// to a human when it detects ambiguity.
type ClarificationMessage struct {
	Question       string   `json:"question"`
	Options        []string `json:"options,omitempty"`
	ContextSummary string   `json:"context_summary"`
	TaskID         string   `json:"task_id"`
	RequestedAt    time.Time `json:"requested_at"`
}

// ClarificationResponse carries the human answer.
type ClarificationResponse struct {
	Answer   string `json:"answer"`
	Selected string `json:"selected,omitempty"`
}

// ClarificationInterface defines how the agent requests clarification
// from a human. Implementations may block until the response arrives
// or the context is cancelled.
type ClarificationInterface interface {
	RequestClarification(ctx context.Context, msg ClarificationMessage) (ClarificationResponse, error)
}

// BlockingClarificationHandler implements ClarificationInterface by
// creating a HUMAN subtask and blocking the parent task until the
// subtask is resolved.
type BlockingClarificationHandler struct {
	store models.KanbanStore
	sink  models.EventSink
}

// NewBlockingClarificationHandler returns a handler that suspends the
// session by blocking the task with a HUMAN subtask.
func NewBlockingClarificationHandler(store models.KanbanStore, sink models.EventSink) *BlockingClarificationHandler {
	return &BlockingClarificationHandler{store: store, sink: sink}
}

// RequestClarification creates a HUMAN subtask for the clarification
// question. The parent task moves to BLOCKED until the subtask is
// completed with an answer.
func (h *BlockingClarificationHandler) RequestClarification(ctx context.Context, msg ClarificationMessage) (ClarificationResponse, error) {
	detail := buildClarificationDetail(msg)

	description := FormatForHuman(HITLMessage{
		Summary:  "Clarification needed from human",
		Action:   "Answer the question below. Add your response as a comment on this subtask and mark it COMPLETED.",
		Urgency:  "blocking",
		Detail:   detail,
	})

	_, subtasks, err := h.store.BlockTaskWithSubtasks(ctx, msg.TaskID, time.Now(), []models.DraftTask{{
		Title:       "Clarification required: " + truncate(msg.Question, 80),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		return ClarificationResponse{}, fmt.Errorf("create clarification subtask: %w", err)
	}

	if len(subtasks) == 0 {
		return ClarificationResponse{}, fmt.Errorf("no clarification subtask created")
	}

	if h.sink != nil {
		_ = h.sink.Emit(ctx, models.Event{
			ProjectID: "",
			Type:      "CLARIFICATION_REQUESTED",
			Payload:   truncate(detail, 1000),
		})
	}

	return ClarificationResponse{}, nil
}

func buildClarificationDetail(msg ClarificationMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", msg.Question)
	if len(msg.Options) > 0 {
		b.WriteString("\nOptions:\n")
		for i, opt := range msg.Options {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, opt)
		}
	}
	if msg.ContextSummary != "" {
		fmt.Fprintf(&b, "\nContext: %s\n", msg.ContextSummary)
	}
	return b.String()
}

// RequestClarificationFromAgent is a convenience function that creates a
// clarification request within the agentic loop. It suspends the session
// until a human provides an answer.
func (w *Worker) RequestClarificationFromAgent(
	ctx context.Context,
	task models.Task,
	question string,
	options []string,
	contextSummary string,
) error {
	handler := NewBlockingClarificationHandler(w.store, w.sink)
	msg := ClarificationMessage{
		Question:       question,
		Options:        options,
		ContextSummary: contextSummary,
		TaskID:         task.ID,
		RequestedAt:    time.Now(),
	}
	_, err := handler.RequestClarification(ctx, msg)
	if err != nil {
		w.emit(ctx, task, "ERROR", fmt.Sprintf("clarification request failed: %v", err))
		return err
	}
	w.emit(ctx, task, "CLARIFICATION_REQUESTED", truncate(question, 500))
	return nil
}
