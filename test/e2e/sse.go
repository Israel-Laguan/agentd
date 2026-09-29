//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEEvent represents a server-sent event from /api/v1/events/stream.
//
// Type is the lowercased event name from the frame's "event:" line, which
// internal/api/sse/stream.go's eventName maps from the internal type (RESULT
// becomes "task_updated", LOG_CHUNK becomes "log_chunk", and anything
// unmapped falls through to strings.ToLower of the internal type).
type SSEEvent struct {
	Type string          // e.g. "log_chunk", "task_updated", "warning"
	Data json.RawMessage // Raw JSON data
}

// signalPayload is the decoded body of an SSE frame's "data:" line
// (internal/api/sse/stream.go's writeEvent marshals exactly these three keys).
type signalPayload struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Signal decodes the event's data line into its topic/type/payload.
func (e *SSEEvent) Signal() (signalPayload, error) {
	var out signalPayload
	if err := json.Unmarshal(e.Data, &out); err != nil {
		return out, fmt.Errorf("decode sse signal: %w", err)
	}
	return out, nil
}

// SSEReader reads from the /api/v1/events/stream endpoint.
//
// A single background goroutine ("pump") owns the response body and decodes
// whole frames, pushing them onto a buffered channel. Reads are served from
// that channel. An earlier version instead spawned a goroutine per
// NextEvent call and abandoned it on timeout — which silently *consumed and
// discarded* the next event, because the orphan kept reading the shared
// scanner. Any timeout-then-read-more sequence (notably J14, which drains
// until the stream goes quiet) could therefore lose frames it was supposed
// to see. The pump makes a read timeout purely a timeout.
type SSEReader struct {
	resp    *http.Response
	cancel  context.CancelFunc
	events  chan readResult
	closeCh chan struct{}
	// readErr records why the pump stopped, so a read that returns no event
	// can report EOF rather than an opaque timeout.
	readErr error
}

type readResult struct {
	event *SSEEvent
	err   error
}

// NewSSEReader opens the SSE endpoint, optionally scoped to one project's
// events, and returns a reader. The caller should call Close() when done.
func NewSSEReader(ctx context.Context, client *APIClient, projectID string) (*SSEReader, error) {
	readCtx, cancel := context.WithCancel(ctx)
	url := client.baseURL + "/api/v1/events/stream"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	req, err := http.NewRequestWithContext(readCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	resp, err := client.client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("SSE endpoint returned %d", resp.StatusCode)
	}

	r := &SSEReader{
		resp:    resp,
		cancel:  cancel,
		events:  make(chan readResult, sseEventBuffer),
		closeCh: make(chan struct{}),
	}
	go r.pump(bufio.NewScanner(resp.Body))
	return r, nil
}

// sseEventBuffer bounds how far the pump may run ahead of the consumer. Deep
// enough that a burst of task events never blocks the reader mid-frame.
const sseEventBuffer = 256

// pump decodes frames off the body until the stream ends, then closes the
// events channel. It is the only reader of the body.
func (r *SSEReader) pump(scanner *bufio.Scanner) {
	defer close(r.events)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		eventType := strings.TrimPrefix(line, "event: ")

		// Expect a data line next.
		if !scanner.Scan() {
			r.readErr = io.EOF
			return
		}
		dataLine := scanner.Text()
		if !strings.HasPrefix(dataLine, "data: ") {
			// Malformed frame; the type line is already consumed, so keep
			// scanning rather than desynchronising on it.
			continue
		}
		event := &SSEEvent{
			Type: eventType,
			Data: json.RawMessage(strings.TrimPrefix(dataLine, "data: ")),
		}
		select {
		case r.events <- readResult{event: event}:
		case <-r.closeCh:
			return
		}
	}
	if err := scanner.Err(); err != nil {
		r.readErr = err
		return
	}
	r.readErr = io.EOF
}

// NextEvent reads the next SSE event, giving up after timeout. On timeout the
// stream stays intact: no event is consumed, so a later call still sees the
// next frame.
func (r *SSEReader) NextEvent(ctx context.Context, timeout time.Duration) (*SSEEvent, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-r.events:
		if !ok {
			if r.readErr != nil && !errors.Is(r.readErr, io.EOF) {
				return nil, r.readErr
			}
			return nil, io.EOF
		}
		return res.event, res.err
	case <-timer.C:
		return nil, fmt.Errorf("SSE read timeout")
	}
}

// DrainEvents reads events until timeout elapses with no new event, returning
// everything received in arrival order. This suits J14, which asserts on a
// burst of lifecycle signals and their relative order, rather than on any
// single event arriving within a window (what NextEvent is for).
//
// A quiet period is treated as end-of-stream: a completing task's frames
// arrive within milliseconds, so an idle gap means the burst is over.
func (r *SSEReader) DrainEvents(ctx context.Context, quietPeriod, maxWait time.Duration) []SSEEvent {
	var events []SSEEvent
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		event, err := r.NextEvent(ctx, quietPeriod)
		if err != nil {
			return events
		}
		events = append(events, *event)
	}
	return events
}

// Close closes the SSE reader.
func (r *SSEReader) Close() error {
	close(r.closeCh)
	r.cancel()
	return r.resp.Body.Close()
}
