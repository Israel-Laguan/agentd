package queue

import (
	"context"
	"time"

	"agentd/internal/models"
)

// TokenUsageEventSource lists persisted per-call token usage for rolling budget hydration.
type TokenUsageEventSource interface {
	ListTokenUsageEventsSince(ctx context.Context, since time.Time) ([]models.TokenUsageEvent, error)
}
