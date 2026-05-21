package worker

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"agentd/internal/gateway"
)

const prePlanCheckpointLabel = "pre_plan"

// SessionCheckpoint captures a point-in-time copy of conversation history.
type SessionCheckpoint struct {
	ID        string
	SessionID string
	CreatedAt time.Time
	Messages  []gateway.PromptMessage
}

// maxCheckpointsPerSession bounds in-memory checkpoint growth per session.
const maxCheckpointsPerSession = 32

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
	mu            sync.RWMutex
	checkpoints   map[string]*SessionCheckpoint
	sessionOrder  map[string][]string
	nextID        int
}

// NewMemoryCheckpointStore returns an in-memory checkpoint store.
func NewMemoryCheckpointStore() CheckpointStore {
	return &memoryCheckpointStore{
		checkpoints:  make(map[string]*SessionCheckpoint),
		sessionOrder: make(map[string][]string),
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
	order := append(s.sessionOrder[sessionID], id)
	for len(order) > maxCheckpointsPerSession {
		oldest := order[0]
		order = order[1:]
		delete(s.checkpoints, oldest)
	}
	s.sessionOrder[sessionID] = order
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

// SessionCheckpointer stores labeled in-memory checkpoints for a single agentic session.
type SessionCheckpointer struct {
	sessionID string
	labels    map[string][]gateway.PromptMessage
	mu        sync.Mutex
}

// NewSessionCheckpointer returns a per-session labeled checkpoint store.
func NewSessionCheckpointer(sessionID string) *SessionCheckpointer {
	return &SessionCheckpointer{
		sessionID: sessionID,
		labels:    make(map[string][]gateway.PromptMessage),
	}
}

// Checkpoint saves a deep copy of messages under label (overwrites prior label).
func (c *SessionCheckpointer) Checkpoint(label string, messages []gateway.PromptMessage) error {
	if c == nil {
		return fmt.Errorf("checkpoint: checkpointer is nil")
	}
	if c.sessionID == "" {
		return fmt.Errorf("checkpoint: sessionID required")
	}
	if label == "" {
		return fmt.Errorf("checkpoint: label required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.labels[label] = clonePromptMessages(messages)
	return nil
}

// BranchFrom restores messages from a labeled checkpoint; post-checkpoint turns are discarded.
func (c *SessionCheckpointer) BranchFrom(label string, messages *[]gateway.PromptMessage) error {
	if c == nil {
		return fmt.Errorf("checkpoint: checkpointer is nil")
	}
	if messages == nil {
		return fmt.Errorf("checkpoint: messages required")
	}
	c.mu.Lock()
	saved, ok := c.labels[label]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("checkpoint label %q not found for session %q", label, c.sessionID)
	}
	*messages = clonePromptMessages(saved)
	return nil
}

// List returns sorted checkpoint labels for this session.
func (c *SessionCheckpointer) List() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.labels))
	for label := range c.labels {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}
