package main

import (
	"net/http"
	"sync/atomic"
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
