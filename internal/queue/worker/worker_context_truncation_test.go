package worker

import (
	"testing"

	"agentd/internal/gateway"
)

func TestNewWorker_KeepsContextTruncation(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{ContextTruncation: strategy})

	if w.contextTruncation != strategy {
		t.Fatalf("worker contextTruncation = %#v, want %#v", w.contextTruncation, strategy)
	}
}
