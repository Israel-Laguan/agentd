package worker

import (
	"context"
	"testing"
	"time"
)

func TestCancelRegistry(t *testing.T) {
	canceller := NewCancelRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	canceller.Register("task", cancel)
	if !canceller.Cancel("task") {
		t.Fatal("Cancel() = false")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled")
	}
	canceller.Deregister("task")
	if canceller.Cancel("task") {
		t.Fatal("Cancel() after deregister = true")
	}
}
