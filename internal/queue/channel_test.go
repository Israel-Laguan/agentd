package queue

import (
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

func validMsg() InboundMessage {
	return InboundMessage{
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		Role:       MessageRoleUser,
		Content:    "hello",
		ReceivedAt: time.Now(),
	}
}

func TestValidate_ValidMessage(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{MaxMessageSize: 1024})
	if err := g.Validate(validMsg()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		patch func(*InboundMessage)
	}{
		{"missing session_id", func(m *InboundMessage) { m.SessionID = "" }},
		{"missing turn_id", func(m *InboundMessage) { m.TurnID = "" }},
		{"invalid role", func(m *InboundMessage) { m.Role = "bogus" }},
		{"empty content", func(m *InboundMessage) { m.Content = "   " }},
		{"zero received_at", func(m *InboundMessage) { m.ReceivedAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewChannelGate(config.ChannelConfig{})
			msg := validMsg()
			tt.patch(&msg)
			err := g.Validate(msg)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !errors.Is(err, models.ErrMessageInvalid) {
				t.Fatalf("expected ErrMessageInvalid, got: %v", err)
			}
		})
	}
}

func TestValidate_MessageTooLarge(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{MaxMessageSize: 10})
	msg := validMsg()
	msg.Content = strings.Repeat("x", 11)
	err := g.Validate(msg)
	if err == nil {
		t.Fatal("expected size error")
	}
	if !errors.Is(err, models.ErrMessageTooLarge) {
		t.Fatalf("expected ErrMessageTooLarge, got: %v", err)
	}
}

func TestValidate_UnlimitedSize(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{MaxMessageSize: 0})
	msg := validMsg()
	msg.Content = strings.Repeat("x", 10_000_000)
	if err := g.Validate(msg); err != nil {
		t.Fatalf("unlimited size should pass: %v", err)
	}
}

func TestAdmit_Ack(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{MaxMessageSize: 1024})
	result := g.Admit(validMsg())
	if result.Disposition != Ack {
		t.Fatalf("expected Ack, got Nack: %v", result.Err)
	}
}

func TestAdmit_NackOnValidation(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{MaxMessageSize: 5})
	msg := validMsg()
	msg.Content = "this is way too long"
	result := g.Admit(msg)
	if result.Disposition != Nack {
		t.Fatal("expected Nack for oversized message")
	}
	if !errors.Is(result.Err, models.ErrDispatchNack) {
		t.Fatalf("expected ErrDispatchNack, got: %v", result.Err)
	}
}

func TestAdmit_RateLimit(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{
		MaxMessageSize: 1024,
		RateLimit:      2,
		RateWindow:     60,
	})
	msg := validMsg()

	r1 := g.Admit(msg)
	if r1.Disposition != Ack {
		t.Fatalf("first request should ack: %v", r1.Err)
	}
	msg.TurnID = "turn-2"
	r2 := g.Admit(msg)
	if r2.Disposition != Ack {
		t.Fatalf("second request should ack: %v", r2.Err)
	}
	msg.TurnID = "turn-3"
	r3 := g.Admit(msg)
	if r3.Disposition != Nack {
		t.Fatal("third request should be nacked (rate limit)")
	}
	if !errors.Is(r3.Err, models.ErrDispatchNack) {
		t.Fatalf("expected ErrDispatchNack, got: %v", r3.Err)
	}
}

func TestAdmit_RateLimitIsolatesSessions(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{
		MaxMessageSize: 1024,
		RateLimit:      1,
		RateWindow:     60,
	})
	msg1 := validMsg()
	msg1.SessionID = "sess-A"
	r1 := g.Admit(msg1)
	if r1.Disposition != Ack {
		t.Fatalf("session A first should ack: %v", r1.Err)
	}

	msg2 := validMsg()
	msg2.SessionID = "sess-B"
	r2 := g.Admit(msg2)
	if r2.Disposition != Ack {
		t.Fatalf("session B first should ack: %v", r2.Err)
	}

	msg1.TurnID = "turn-2"
	r3 := g.Admit(msg1)
	if r3.Disposition != Nack {
		t.Fatal("session A second should be nacked")
	}
}

func TestAdmit_RateLimitDisabled(t *testing.T) {
	g := NewChannelGate(config.ChannelConfig{
		MaxMessageSize: 1024,
		RateLimit:      0,
		RateWindow:     60,
	})
	msg := validMsg()
	for i := range 100 {
		msg.TurnID = "turn-" + string(rune('A'+i))
		r := g.Admit(msg)
		if r.Disposition != Ack {
			t.Fatalf("request %d should ack with unlimited rate: %v", i, r.Err)
		}
	}
}

func TestMessageRole_Valid(t *testing.T) {
	tests := []struct {
		role MessageRole
		want bool
	}{
		{MessageRoleUser, true},
		{MessageRoleSystem, true},
		{"admin", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("MessageRole(%q).Valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

func TestTaskToInbound(t *testing.T) {
	now := time.Now()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-1", UpdatedAt: now},
		ProjectID:   "proj-1",
		Description: "do things",
	}
	msg := TaskToInbound(task)
	if msg.SessionID != "proj-1" {
		t.Fatalf("SessionID = %q, want proj-1", msg.SessionID)
	}
	if msg.TurnID != "task-1" {
		t.Fatalf("TurnID = %q, want task-1", msg.TurnID)
	}
	if msg.Role != MessageRoleSystem {
		t.Fatalf("Role = %q, want system", msg.Role)
	}
	if msg.Content != "do things" {
		t.Fatalf("Content = %q, want 'do things'", msg.Content)
	}
	if msg.ReceivedAt != now {
		t.Fatalf("ReceivedAt mismatch")
	}
}
