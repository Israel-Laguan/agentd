package main

import (
	"context"
	"net/http"
	"sync/atomic"

	"agentd/internal/config"
)

// warmupGate answers GET /health with warming_up while the LLM warmup runs,
// so the API is reachable from the moment it listens instead of refusing
// connections until the provider answers (B-083). Every other route is served
// as usual; the queue daemon still starts only after the warmup succeeds.
type warmupGate struct {
	next    http.Handler
	warming atomic.Bool
}

func newWarmupGate(next http.Handler) *warmupGate {
	g := &warmupGate{next: next}
	g.warming.Store(true)
	return g
}

func (g *warmupGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.warming.Load() && r.Method == http.MethodGet && r.URL.Path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","warming_up":true}` + "\n"))
		return
	}
	g.next.ServeHTTP(w, r)
}

// warmUpWhileServing runs the LLM warmup while the API already serves, then
// lets /health report plain ok. On failure it stops the API server and reports
// false with the error to return. A canceled ctx means an interrupt, or an API
// server failure that already called stop(): that is reported (nil for a clean
// interrupt), not the warmup it cut short.
func warmUpWhileServing(
	ctx context.Context, stop context.CancelFunc, apiErrCh <-chan error,
	gate *warmupGate, deps runtimeDeps, cfg config.Config, startOpts *startOptions,
) (bool, error) {
	err := warmupLLMIfNeeded(ctx, deps.gateway, cfg.Gateway, startOpts.skipLLMWarmup, cfg.Gateway.WarmupEnabled)
	if err == nil {
		gate.warming.Store(false)
		return true, nil
	}
	interrupted := ctx.Err() != nil
	stop()
	drainErr := drainAPIServerError(apiErrCh)
	if interrupted {
		return false, drainErr
	}
	return false, err
}
