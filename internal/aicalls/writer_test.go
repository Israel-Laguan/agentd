package aicalls

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type fakeSink struct {
	mu      sync.Mutex
	batches [][]models.AICall
	gate    chan struct{} // when set, InsertAICalls blocks until it is closed
	err     error
}

func (f *fakeSink) InsertAICalls(_ context.Context, calls []models.AICall) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.batches = append(f.batches, append([]models.AICall(nil), calls...))
	return nil
}

func (f *fakeSink) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.batches {
		n += len(b)
	}
	return n
}

func rec(i int) spec.CallRecord {
	return spec.CallRecord{
		At: time.Unix(int64(i), 0).UTC(), Kind: spec.KindGenerate, Purpose: spec.PurposeExecTurn, Stage: "exec",
		Provider: "p", Model: "m", Attempt: spec.AttemptFirst, Outcome: spec.OutcomeOK, Tokens: i, Latency: 1500 * time.Millisecond,
	}
}

func closeWriter(t *testing.T, w *Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// A stalled database must never stall the request path: a flood of records
// returns at once and what does not fit is counted, not lost silently.
func TestWriterFloodNeverBlocksAndCountsDrops(t *testing.T) {
	sink := &fakeSink{gate: make(chan struct{})}
	w := NewWriter(sink, Options{Buffer: 4, BatchSize: 2, Interval: time.Hour})
	const flood = 1000
	done := make(chan struct{})
	go func() {
		for i := 0; i < flood; i++ {
			w.Record(rec(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked the caller while the sink was stalled")
	}
	if w.Dropped() == 0 {
		t.Fatal("Dropped() = 0 after a flood into a 4-slot buffer")
	}
	close(sink.gate)
	closeWriter(t, w)
	if got := uint64(sink.total()) + w.Dropped(); got != flood {
		t.Fatalf("stored %d + dropped %d = %d, want %d", sink.total(), w.Dropped(), got, flood)
	}
}

func TestWriterBatchesBySizeAndFlushesOnClose(t *testing.T) {
	sink := &fakeSink{}
	w := NewWriter(sink, Options{Buffer: 64, BatchSize: 3, Interval: time.Hour})
	for i := 0; i < 7; i++ {
		w.Record(rec(i))
	}
	closeWriter(t, w)
	if sink.total() != 7 || w.Dropped() != 0 {
		t.Fatalf("stored %d dropped %d, want 7 and 0", sink.total(), w.Dropped())
	}
	for _, b := range sink.batches {
		if len(b) > 3 {
			t.Fatalf("batch of %d exceeds BatchSize 3", len(b))
		}
	}
}

func TestWriterFlushesOnInterval(t *testing.T) {
	sink := &fakeSink{}
	w := NewWriter(sink, Options{Buffer: 64, BatchSize: 100, Interval: 10 * time.Millisecond})
	defer closeWriter(t, w)
	w.Record(rec(1))
	deadline := time.Now().Add(2 * time.Second)
	for sink.total() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("record not flushed by the interval")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWriterMapsTheRecordAndCountsFailedInserts(t *testing.T) {
	sink := &fakeSink{}
	w := NewWriter(sink, Options{Buffer: 8, BatchSize: 8, Interval: time.Hour})
	r := rec(5)
	r.Cascade, r.Truncated, r.CachedTokens, r.CacheWrite, r.TaskID, r.CorrelationID = true, true, 3, 2, "t1", "req"
	w.Record(r)
	closeWriter(t, w)
	got := sink.batches[0][0]
	if got.Purpose != "exec.turn" || got.Stage != "exec" || got.LatencyMS != 1500 || !got.Cascade || !got.Truncated ||
		got.CachedTokens != 3 || got.CacheWriteTokens != 2 || got.TaskID != "t1" || got.CorrelationID != "req" || got.Attempt != "first" {
		t.Fatalf("mapped = %+v", got)
	}

	failing := &fakeSink{err: errors.New("disk full")}
	w2 := NewWriter(failing, Options{Buffer: 8, BatchSize: 8, Interval: time.Hour})
	w2.Record(rec(1))
	w2.Record(rec(2))
	closeWriter(t, w2)
	if w2.Dropped() != 2 {
		t.Fatalf("Dropped() = %d after a failed insert of 2, want 2", w2.Dropped())
	}
}

func TestWriterRecordAfterCloseCountsAndDoesNotPanic(t *testing.T) {
	w := NewWriter(&fakeSink{}, Options{})
	closeWriter(t, w)
	w.Record(rec(1))
	if w.Dropped() != 1 {
		t.Fatalf("Dropped() = %d, want 1", w.Dropped())
	}
	closeWriter(t, w) // closing twice is harmless
}
