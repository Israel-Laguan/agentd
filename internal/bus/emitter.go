package bus

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// EventEmitter persists events and broadcasts them to live subscribers.
type EventEmitter struct {
	store models.KanbanStore
	bus   Bus
}

// NewEventEmitter wires the durable event store to the live event bus.
func NewEventEmitter(store models.KanbanStore, b Bus) *EventEmitter {
	return &EventEmitter{store: store, bus: b}
}

// Emit writes the event first, then publishes it best-effort for live clients.
func (e *EventEmitter) Emit(ctx context.Context, evt models.Event) error {
	if err := e.store.AppendEvent(ctx, evt); err != nil {
		return err
	}
	e.publish(ctx, GlobalTopic, evt)
	e.publish(ctx, eventTopic(evt), evt)
	if evt.TaskID.Valid {
		e.publish(ctx, fmt.Sprintf("project:%s", evt.ProjectID), evt)
	}
	return nil
}

func (e *EventEmitter) publish(ctx context.Context, topic string, evt models.Event) {
	e.bus.Publish(ctx, Signal{
		Topic:   topic,
		Type:    string(evt.Type),
		Payload: evt.Payload,
	})
}

func eventTopic(evt models.Event) string {
	if evt.TaskID.Valid {
		return fmt.Sprintf("task:%s", evt.TaskID.String)
	}
	return fmt.Sprintf("project:%s", evt.ProjectID)
}
