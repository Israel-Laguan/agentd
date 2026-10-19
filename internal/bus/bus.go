package bus

import "context"

const GlobalTopic = "*"

// Signal is a transport-neutral notification routed to SSE and internal consumers.
// TaskID and ProjectID name the entity the signal is about; they are empty for
// signals that belong to neither (agent updates).
type Signal struct {
	Topic     string
	Type      string
	Payload   string
	TaskID    string
	ProjectID string
}

// Bus routes best-effort live events inside the daemon process.
type Bus interface {
	Publish(ctx context.Context, sig Signal)
	Subscribe(topic string, buffer int) (<-chan Signal, func())
}
