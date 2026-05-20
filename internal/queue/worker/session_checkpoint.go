package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agentd/internal/gateway"
)

// SessionCheckpoint captures a point-in-time copy of conversation history.
type SessionCheckpoint struct {
	ID        string
	SessionID string
	CreatedAt time.Time
	Messages  []gateway.PromptMessage
}

// CheckpointStore persists session checkpoints for restore before history edits.
type CheckpointStore interface {
	Create(ctx context.Context, sessionID string, messages []gateway.PromptMessage) (checkpointID string, err error)
	Get(ctx context.Context, checkpointID string) (*SessionCheckpoint, error)
}

// clonePromptMessages returns a deep copy of messages for checkpoint snapshots.
func clonePromptMessages(messages []gateway.PromptMessage) []gateway.PromptMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]gateway.PromptMessage, len(messages))
	for i, m := range messages {
		out[i] = m
		if len(m.ToolCalls) > 0 {
			out[i].ToolCalls = append([]gateway.ToolCall(nil), m.ToolCalls...)
		}
	}
	return out
}

// memoryCheckpointStore is an in-process CheckpointStore for agentic sessions.
type memoryCheckpointStore struct {
	mu           sync.RWMutex
	checkpoints  map[string]*SessionCheckpoint
	nextID       int
}

// NewMemoryCheckpointStore returns an in-memory checkpoint store.
func NewMemoryCheckpointStore() CheckpointStore {
	return &memoryCheckpointStore{
		checkpoints: make(map[string]*SessionCheckpoint),
	}
}

func (s *memoryCheckpointStore) Create(_ context.Context, sessionID string, messages []gateway.PromptMessage) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("checkpoint: sessionID required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("%s:cp:%d", sessionID, s.nextID)
	s.checkpoints[id] = &SessionCheckpoint{
		ID:        id,
		SessionID: sessionID,
		CreatedAt: time.Now().UTC(),
		Messages:  clonePromptMessages(messages),
	}
	return id, nil
}

func (s *memoryCheckpointStore) Get(_ context.Context, checkpointID string) (*SessionCheckpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.checkpoints[checkpointID]
	if !ok {
		return nil, fmt.Errorf("checkpoint %q not found", checkpointID)
	}
	dup := *cp
	dup.Messages = clonePromptMessages(cp.Messages)
	return &dup, nil
}
