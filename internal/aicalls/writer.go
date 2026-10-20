// Package aicalls persists the gateway's per-attempt call records (T-044)
// without ever slowing the request path.
package aicalls

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// Sink stores a batch of records. The kanban store implements it.
type Sink interface {
	InsertAICalls(ctx context.Context, calls []models.AICall) error
}

// Options tune the writer. Zero values take the defaults.
type Options struct {
	Buffer    int           // records held between the request path and the writer (default 1024)
	BatchSize int           // records per insert (default 100)
	Interval  time.Duration // longest a record waits for a full batch (default 2s)
}

const (
	defaultBuffer    = 1024
	defaultBatchSize = 100
	defaultInterval  = 2 * time.Second
	insertTimeout    = 10 * time.Second
)

// Writer is a spec.Recorder that queues records on a bounded channel and
// inserts them in batches from one goroutine. Record never blocks and never
// panics: when the buffer is full, or the writer is closed, the record is
// dropped and counted.
type Writer struct {
	sink    Sink
	opts    Options
	ch      chan spec.CallRecord
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	closed  atomic.Bool
	dropped atomic.Uint64
}

var _ spec.Recorder = (*Writer)(nil)

// NewWriter starts the writer goroutine. Call Close to flush it.
func NewWriter(sink Sink, opts Options) *Writer {
	if opts.Buffer <= 0 {
		opts.Buffer = defaultBuffer
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	w := &Writer{
		sink: sink, opts: opts, ch: make(chan spec.CallRecord, opts.Buffer),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Record queues one record, or counts it as dropped.
func (w *Writer) Record(rec spec.CallRecord) {
	if w.closed.Load() {
		w.dropped.Add(1)
		return
	}
	select {
	case w.ch <- rec:
	default:
		w.dropped.Add(1)
	}
}

// Dropped is how many records were discarded since the writer started: buffer
// full, written after Close, or lost to a failed insert.
func (w *Writer) Dropped() uint64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}

// Close stops accepting records, writes what is queued, and waits for the
// writer to finish or ctx to end. It is safe to call more than once.
func (w *Writer) Close(ctx context.Context) error {
	w.once.Do(func() {
		w.closed.Store(true)
		close(w.stop)
	})
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.opts.Interval)
	defer ticker.Stop()
	batch := make([]models.AICall, 0, w.opts.BatchSize)
	for {
		select {
		case rec := <-w.ch:
			batch = w.add(batch, rec)
		case <-ticker.C:
			batch = w.flush(batch)
		case <-w.stop:
			w.drain(batch)
			return
		}
	}
}

// add appends a record and flushes once the batch is full.
func (w *Writer) add(batch []models.AICall, rec spec.CallRecord) []models.AICall {
	batch = append(batch, toAICall(rec))
	if len(batch) >= w.opts.BatchSize {
		return w.flush(batch)
	}
	return batch
}

// drain writes the batch in hand and everything still queued.
func (w *Writer) drain(batch []models.AICall) {
	for {
		select {
		case rec := <-w.ch:
			batch = w.add(batch, rec)
		default:
			w.flush(batch)
			return
		}
	}
}

// flush inserts the batch and returns it emptied. A failed insert is logged
// and counted as dropped: retrying would hold the buffer against a sick database.
func (w *Writer) flush(batch []models.AICall) []models.AICall {
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()
	if err := w.sink.InsertAICalls(ctx, batch); err != nil {
		w.dropped.Add(uint64(len(batch)))
		slog.Warn("ai call records dropped: insert failed", "records", len(batch), "error", err)
	}
	return batch[:0]
}

func toAICall(r spec.CallRecord) models.AICall {
	return models.AICall{
		At: r.At, Kind: string(r.Kind), Purpose: string(r.Purpose), Stage: r.Stage, Role: string(r.Role),
		TaskID: r.TaskID, AgentID: r.AgentID, CorrelationID: r.CorrelationID, Provider: r.Provider,
		Model: r.Model, Attempt: string(r.Attempt), Cascade: r.Cascade, Outcome: string(r.Outcome),
		Truncated: r.Truncated, MessageCount: r.MessageCount, ToolCount: r.ToolCount,
		InputChars: r.InputChars, OutputChars: r.OutputChars, Tokens: r.Tokens,
		CachedTokens: r.CachedTokens, CacheWriteTokens: r.CacheWrite, LatencyMS: r.Latency.Milliseconds(),
	}
}
