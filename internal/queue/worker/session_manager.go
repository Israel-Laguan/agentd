package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// SessionManager coordinates in-task session lifecycle: topic anchor, comment polling, archive on drift.
type SessionManager struct {
	sessionID     string
	generation    int
	topicAnchor   string
	commentCursor time.Time
	checkpoints   CheckpointStore
}

// NewSessionManager creates a session manager for a task attempt.
func NewSessionManager(sessionID, initialTopic string, store CheckpointStore) *SessionManager {
	return &SessionManager{
		sessionID:   sessionID,
		topicAnchor: strings.TrimSpace(initialTopic),
		checkpoints: store,
	}
}

// TopicAnchor returns the current session topic text.
func (sm *SessionManager) TopicAnchor() string {
	if sm == nil {
		return ""
	}
	return sm.topicAnchor
}

// Generation returns how many times the session was reset due to topic drift.
func (sm *SessionManager) Generation() int {
	if sm == nil {
		return 0
	}
	return sm.generation
}

// AdvanceTopic updates the ongoing topic anchor after non-drift human input.
func (sm *SessionManager) AdvanceTopic(topic string) {
	if sm == nil {
		return
	}
	topic = strings.TrimSpace(topic)
	if topic != "" {
		sm.topicAnchor = topic
	}
}

// PollNewHumanInput returns the latest human/frontdesk comment since the last topic check.
func (sm *SessionManager) PollNewHumanInput(
	ctx context.Context,
	store models.KanbanStore,
	taskID string,
) (body string, ok bool) {
	if sm == nil || store == nil {
		return "", false
	}
	comments, err := store.ListCommentsSince(ctx, taskID, sm.commentCursor)
	if err != nil || len(comments) == 0 {
		return "", false
	}
	var latest *models.Comment
	for i := range comments {
		c := comments[i]
		sm.advanceCommentCursor(c.UpdatedAt)
		if !isHumanSteeringComment(c.Author) {
			continue
		}
		if latest == nil || c.UpdatedAt.After(latest.UpdatedAt) {
			latest = &c
		}
	}
	if latest == nil {
		return "", false
	}
	return strings.TrimSpace(latest.Body), true
}

func (sm *SessionManager) advanceCommentCursor(updatedAt time.Time) {
	if updatedAt.After(sm.commentCursor) {
		sm.commentCursor = updatedAt
	}
}

func isHumanSteeringComment(author models.CommentAuthor) bool {
	switch author {
	case models.CommentAuthorUser, models.CommentAuthorFrontdesk:
		return true
	default:
		return false
	}
}

// topicDriftPayload is persisted on TOPIC_DRIFT events.
type topicDriftPayload struct {
	Generation    int    `json:"generation"`
	PriorTopic    string `json:"prior_topic"`
	NewInput      string `json:"new_input"`
	CheckpointID  string `json:"checkpoint_id"`
}

// ArchiveAndReset checkpoints the current transcript, emits TOPIC_DRIFT, and starts a fresh session.
func (sm *SessionManager) ArchiveAndReset(
	ctx context.Context,
	w *Worker,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	newInput string,
) (checkpointID string, err error) {
	if sm == nil || w == nil || messages == nil {
		return "", fmt.Errorf("session manager: invalid reset state")
	}
	priorTopic := sm.topicAnchor
	if sm.checkpoints != nil {
		checkpointID, err = sm.checkpoints.Create(ctx, sm.sessionID, *messages)
		if err != nil {
			return "", fmt.Errorf("archive session: %w", err)
		}
	}
	sm.generation++
	sm.topicAnchor = strings.TrimSpace(newInput)

	payload, _ := json.Marshal(topicDriftPayload{
		Generation:   sm.generation,
		PriorTopic:   priorTopic,
		NewInput:     newInput,
		CheckpointID: checkpointID,
	})
	w.emit(ctx, task, string(models.EventTypeTopicDrift), string(payload))

	fresh := w.assembleAgenticSystemPromptWithUserContent(ctx, task, project, profile, newInput)
	*messages = fresh
	return checkpointID, nil
}

// extractAnchorUserContent returns the first user message content (session seed topic).
func extractAnchorUserContent(messages []gateway.PromptMessage) string {
	for _, m := range messages {
		if m.Role == "user" {
			return strings.TrimSpace(m.Content)
		}
	}
	return ""
}
