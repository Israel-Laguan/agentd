package bus

import "context"

const GlobalTopic = "*"

// Signal is a transport-neutral notification routed to SSE and internal consumers.
type Signal struct {
	Topic   string
	Type    string
	Payload string
}

// Bus routes best-effort live events inside the daemon process.
type Bus interface {
	Publish(ctx context.Context, sig Signal)
	Subscribe(topic string, buffer int) (<-chan Signal, func())
}
