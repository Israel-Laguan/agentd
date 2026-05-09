package bus

import (
	"context"

	"agentd/internal/models"
)

// EventBridge adapts an EventEmitter to the models.EventSink interface.
type EventBridge struct {
	Emitter *EventEmitter
}

func (s EventBridge) Emit(ctx context.Context, evt models.Event) error {
	if s.Emitter == nil {
		return nil
	}
	return s.Emitter.Emit(ctx, evt)
}
