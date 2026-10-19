package agentic

import (
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestNewAgenticContextManager_UsesContextTruncation(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	e := &Engine{config: Config{ContextTruncation: strategy}}

	cm, _ := e.newAgenticContextManager(models.Task{})

	if got := cm.TruncationStrategy(); got != strategy {
		t.Fatalf("context manager strategy = %#v, want %#v", got, strategy)
	}
}
