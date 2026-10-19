package worker

import (
	"testing"

	"agentd/internal/gateway"
)

func TestWorkerAgenticConfig_PassesContextTruncation(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	w := NewWorker(nil, nil, nil, nil, nil, WorkerOptions{ContextTruncation: strategy})

	if got := w.agenticConfig().ContextTruncation; got != strategy {
		t.Fatalf("agentic.Config.ContextTruncation = %#v, want %#v", got, strategy)
	}
}
