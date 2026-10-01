package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"agentd/internal/models"
)

// T-031 / SP-009: the token ledger must never silently drop a write.

type failingSink struct {
	mu    sync.Mutex
	calls int
}

func (s *failingSink) Emit(ctx context.Context, ev models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return errors.New("sink unavailable")
}

// TestEmitLogsDroppedEvent fails before T-031: Emit discarded the sink error
// with `_ =`, so a dropped TOKEN_USAGE event left no trace whatsoever.
func TestEmitLogsDroppedEvent(t *testing.T) {
	sink := &failingSink{}
	w := &Worker{sink: sink}

	logged := captureErrorLogs(t)
	w.Emit(context.Background(),
		models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, ProjectID: "p1"},
		string(models.EventTypeTokenUsage), `{}`)

	if !logged() {
		t.Error("Emit dropped the sink error without logging it; a lost ledger event " +
			"must be visible in the logs")
	}
	if sink.calls < 1 {
		t.Fatalf("sink calls = %d, want >= 1", sink.calls)
	}
}

// captureErrorLogs installs a slog handler that records whether anything was
// logged at Error level, and returns a func reporting that.
func captureErrorLogs(t *testing.T) func() bool {
	t.Helper()
	var mu sync.Mutex
	sawError := false
	prev := slog.Default()
	slog.SetDefault(slog.New(&errRecorder{onError: func() {
		mu.Lock()
		defer mu.Unlock()
		sawError = true
	}}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawError
	}
}

type errRecorder struct {
	onError func()
}

func (r *errRecorder) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

func (r *errRecorder) Handle(ctx context.Context, rec slog.Record) error {
	r.onError()
	return nil
}

func (r *errRecorder) WithAttrs(attrs []slog.Attr) slog.Handler { return r }
func (r *errRecorder) WithGroup(name string) slog.Handler       { return r }
