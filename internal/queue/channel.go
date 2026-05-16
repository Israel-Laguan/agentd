package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

// MessageRole identifies the actor that produced an inbound message.
type MessageRole string

const (
	MessageRoleUser   MessageRole = "user"
	MessageRoleSystem MessageRole = "system"
)

// Valid reports whether r is a recognized message role.
func (r MessageRole) Valid() bool {
	switch r {
	case MessageRoleUser, MessageRoleSystem:
		return true
	default:
		return false
	}
}

// InboundMessage is the canonical contract for every message entering the
// harness through any channel (queue daemon, REST API, future transports).
type InboundMessage struct {
	SessionID  string            `json:"session_id"`
	TurnID     string            `json:"turn_id"`
	Role       MessageRole       `json:"role"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ReceivedAt time.Time         `json:"received_at"`
}

// Disposition is the outcome of a channel admission decision.
type Disposition int

const (
	// Ack means the message was accepted and forwarded to the loop.
	Ack Disposition = iota
	// Nack means the message was rejected; it may be retried or routed
	// to a dead-letter path.
	Nack
)

// DispatchResult carries the outcome of a single message admission.
type DispatchResult struct {
	Disposition Disposition
	Err         error
}

// Channel is the I/O boundary between external transports and the worker
// loop. Implementations normalise raw payloads into InboundMessage and
// enforce size, rate, and structural constraints before forwarding.
type Channel interface {
	Validate(msg InboundMessage) error
	Admit(msg InboundMessage) DispatchResult
}

// ChannelGate is the default Channel implementation that enforces message
// size limits, structural validation, and per-session rate limiting.
type ChannelGate struct {
	maxMessageSize int
	rateLimit      int
	rateWindow     time.Duration

	mu        sync.Mutex
	sessions  map[string]*sessionWindow
	lastSweep time.Time
}

type sessionWindow struct {
	timestamps []time.Time
}

// NewChannelGate creates a ChannelGate from the loaded configuration.
func NewChannelGate(cfg config.ChannelConfig) *ChannelGate {
	maxMessageSize := cfg.MaxMessageSize
	if maxMessageSize < 0 {
		maxMessageSize = 0
	}
	rateLimit := cfg.RateLimit
	if rateLimit < 0 {
		rateLimit = 0
	}
	rateWindow := cfg.RateWindow
	if rateLimit > 0 && rateWindow <= 0 {
		rateWindow = config.DefaultChannelRateWindow
	}
	return &ChannelGate{
		maxMessageSize: maxMessageSize,
		rateLimit:      rateLimit,
		rateWindow:     time.Duration(rateWindow) * time.Second,
		sessions:       make(map[string]*sessionWindow),
	}
}

// Validate checks structural and size constraints without mutating state.
func (g *ChannelGate) Validate(msg InboundMessage) error {
	if strings.TrimSpace(msg.SessionID) == "" {
		return fmt.Errorf("%w: session_id is required", models.ErrMessageInvalid)
	}
	if strings.TrimSpace(msg.TurnID) == "" {
		return fmt.Errorf("%w: turn_id is required", models.ErrMessageInvalid)
	}
	if !msg.Role.Valid() {
		return fmt.Errorf("%w: role %q is not recognized", models.ErrMessageInvalid, msg.Role)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("%w: content must not be empty", models.ErrMessageInvalid)
	}
	if msg.ReceivedAt.IsZero() {
		return fmt.Errorf("%w: received_at must be set", models.ErrMessageInvalid)
	}
	if g.maxMessageSize > 0 && len(msg.Content) > g.maxMessageSize {
		return fmt.Errorf("%w: %d bytes exceeds limit of %d",
			models.ErrMessageTooLarge, len(msg.Content), g.maxMessageSize)
	}
	return nil
}

// Admit validates the message and enforces the per-session rate limit.
// On success it returns Ack; on failure it returns Nack with a wrapped
// sentinel error suitable for dead-letter routing.
func (g *ChannelGate) Admit(msg InboundMessage) DispatchResult {
	if err := g.Validate(msg); err != nil {
		return DispatchResult{
			Disposition: Nack,
			Err:         fmt.Errorf("%w: %v", models.ErrDispatchNack, err),
		}
	}
	if err := g.checkRate(msg.SessionID); err != nil {
		return DispatchResult{
			Disposition: Nack,
			Err:         fmt.Errorf("%w: %v", models.ErrDispatchNack, err),
		}
	}
	return DispatchResult{Disposition: Ack}
}

func (g *ChannelGate) checkRate(sessionID string) error {
	if g.rateLimit <= 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-g.rateWindow)

	if g.lastSweep.IsZero() || now.Sub(g.lastSweep) >= g.rateWindow {
		for sid, win := range g.sessions {
			win.timestamps = pruneOld(win.timestamps, cutoff)
			if len(win.timestamps) == 0 {
				delete(g.sessions, sid)
			}
		}
		g.lastSweep = now
	}

	sw, ok := g.sessions[sessionID]
	if !ok {
		sw = &sessionWindow{}
		g.sessions[sessionID] = sw
	}
	sw.timestamps = pruneOld(sw.timestamps, cutoff)
	if len(sw.timestamps) >= g.rateLimit {
		return fmt.Errorf("%w: session %s exceeded %d requests in %s",
			models.ErrChannelRateLimited, sessionID, g.rateLimit, g.rateWindow)
	}
	sw.timestamps = append(sw.timestamps, now)
	return nil
}

func pruneOld(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for _, t := range ts {
		if !t.Before(cutoff) {
			ts[i] = t
			i++
		}
	}
	return ts[:i]
}

// TaskToInbound converts a claimed Task into the canonical InboundMessage
// so the channel gate can validate it before worker handoff.
// SessionID is the task ID so channel rate limits apply per task, not per
// project (aligned with worker hook SessionID).
func TaskToInbound(t models.Task) InboundMessage {
	content := strings.TrimSpace(t.Description)
	if content == "" {
		content = strings.TrimSpace(t.Title)
	}
	return InboundMessage{
		SessionID:  t.ID,
		TurnID:     t.ID,
		Role:       MessageRoleSystem,
		Content:    content,
		ReceivedAt: t.UpdatedAt,
	}
}

// nackTask transitions a rejected task back to READY so it becomes eligible
// for redelivery on the next dispatch cycle.
func (d *Daemon) nackTask(ctx context.Context, task models.Task) {
	_, err := d.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateReady)
	if err != nil {
		slog.Error("nack: failed to release task", "task_id", task.ID, "error", err)
	}
}

var _ Channel = (*ChannelGate)(nil)
