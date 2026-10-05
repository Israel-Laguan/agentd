package main

import (
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/queue"
)

// breaker.open_timeout must reach the per-provider registry as well as the global
// breaker; before B-012 the provider breakers ignored it.
func TestNewRuntimeDepsPassesOpenTimeoutToProviderBreakers(t *testing.T) {
	cfg := config.Config{ProjectsDir: t.TempDir()}
	cfg.Breaker.OpenTimeout = 10 * time.Second

	deps, err := newRuntimeDeps(cfg, nil)
	if err != nil {
		t.Fatalf("newRuntimeDeps() error = %v", err)
	}

	b := deps.providerBreakers.Get("gemini")
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		b.RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(11 * time.Second)
	if got := b.Admit(); got != queue.AdmissionProbe {
		t.Fatalf("Admit() 11s after tripping with open_timeout=10s = %v, want the probe slot", got)
	}
}
