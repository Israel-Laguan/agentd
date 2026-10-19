package api_test

import (
	"testing"
	"time"

	"agentd/internal/api"
	"agentd/internal/testutil"
)

// T-050: the daemon's http.Server must bound how long a client may hold a
// connection while sending headers, the body, or sitting idle. WriteTimeout
// stays unset because the SSE event stream is long-lived.
func TestNewServerSetsReadAndIdleTimeouts(t *testing.T) {
	srv := api.NewServer(api.ServerDeps{Addr: "127.0.0.1:0", Store: testutil.NewFakeStore()})
	if srv.ReadHeaderTimeout <= 0 || srv.ReadHeaderTimeout > 30*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want (0, 30s]", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout <= 0 || srv.ReadTimeout > 2*time.Minute {
		t.Errorf("ReadTimeout = %v, want (0, 2m]", srv.ReadTimeout)
	}
	if srv.IdleTimeout <= 0 || srv.IdleTimeout > 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want (0, 5m]", srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 so SSE streams are not cut", srv.WriteTimeout)
	}
}
